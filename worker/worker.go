// Copyright 2018 The go-hpb Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The go-hpb is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-hpb is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-hpb. If not, see <http://www.gnu.org/licenses/>.

package worker

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hpb-project/sphinx/network/p2p"
	"github.com/hpb-project/sphinx/network/p2p/discover"

	"github.com/hpb-project/sphinx/blockchain"
	"github.com/hpb-project/sphinx/blockchain/state"
	"github.com/hpb-project/sphinx/blockchain/storage"
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/common/log"
	"github.com/hpb-project/sphinx/config"
	"github.com/hpb-project/sphinx/consensus"
	"github.com/hpb-project/sphinx/event/sub"
	"github.com/hpb-project/sphinx/txpool"
)

const (
	resultQueueSize  = 10
	miningLogAtDepth = 5

	// txChanSize is the size of channel listening to TxPreEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 100000
	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	blockMaxTxs = 5000 * 10
)

// Work is the workers current environment and holds
// all of the current state information
type Work struct {
	config *config.ChainConfig
	signer types.Signer

	state     *state.StateDB // apply state changes here
	tcount    int            // tx count in cycle

	Block *types.Block // the new block

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt

	createdAt time.Time
}

type Result struct {
	Work  *Work
	Block *types.Block
}

// worker is the main object which takes care of applying messages to the new state
type worker struct {
	config *config.ChainConfig
	engine consensus.Engine

	mu sync.Mutex

	mux          *sub.TypeMux
	txpool       *txpool.TxPool
	chainHeadCh  chan bc.ChainHeadEvent
	chainHeadSub sub.Subscription
	recv      chan *Result

	chain   *bc.BlockChain
	chainDb hpbdb.Database

	coinbase common.Address
	extra    []byte

	currentMu sync.Mutex
	current   *Work

	unconfirmed *unconfirmedProofs // set of locally mined blocks pending canonicalness confirmations

	// atomic status counters
	mining int32
	atWork int32
}

func newWorker(config *config.ChainConfig, engine consensus.Engine, coinbase common.Address /*eth Backend,*/, mux *sub.TypeMux) *worker {
	worker := &worker{
		config:      config,
		engine:      engine,
		mux:         mux,
		chainHeadCh: make(chan bc.ChainHeadEvent, chainHeadChanSize),
		chainDb:     nil,
		recv:        make(chan *Result, resultQueueSize),
		chain:       bc.InstanceBlockChain(),
		coinbase:    coinbase,
		unconfirmed: newUnconfirmedProofs(),
	}

	worker.txpool = txpool.GetTxPool()
	worker.chainHeadSub = bc.InstanceBlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)
	// goto listen the event
	go worker.eventListener()
	go worker.handlerSelfMinedBlock()

	return worker
}

func (self *worker) setHpberbase(addr common.Address) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.coinbase = addr
}

func (self *worker) setExtra(extra []byte) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.extra = extra
}

func (self *worker) pending() (*types.Block, *state.StateDB) {
	self.currentMu.Lock()
	defer self.currentMu.Unlock()

	if atomic.LoadInt32(&self.mining) == 0 {
		return types.NewBlock(
			self.current.header,
			self.current.txs,
			self.current.receipts,
		), self.current.state.Copy()
	}
	return self.current.Block, self.current.state.Copy()
}

func (self *worker) pendingBlock() *types.Block {
	self.currentMu.Lock()
	defer self.currentMu.Unlock()

	if atomic.LoadInt32(&self.mining) == 0 {
		return types.NewBlock(
			self.current.header,
			self.current.txs,
			self.current.receipts,
		)
	}
	return self.current.Block
}

func (self *worker) start() {
	self.mu.Lock()
	defer self.mu.Unlock()

	atomic.StoreInt32(&self.mining, 1)
}

func (self *worker) stop() {
	self.mu.Lock()
	defer self.mu.Unlock()
	atomic.StoreInt32(&self.mining, 0)
	atomic.StoreInt32(&self.atWork, 0)
}

func (self *worker) eventListener() {

	//defer self.txSub.Unsubscribe()
	defer self.chainHeadSub.Unsubscribe()

	for {
		// A real event arrived, process interesting content
		select {
		// Handle ChainHeadEvent
		case <-self.chainHeadCh:
			//Todo: self.PreMiner()

		case <-self.chainHeadSub.Err():
			return
		}
	}
}

func (self *worker) handlerSelfMinedBlock() {
	for {
		mustCommitNewWork := true
		for result := range self.recv {
			atomic.AddInt32(&self.atWork, -1)

			if result == nil {
				continue
			}
			block := result.Block
			work := result.Work

			//// Update the block hash in all logs since it is now available and not when the
			//// receipt/log of individual transactions were created.
			//for _, r := range work.receipts {
			//	for _, l := range r.Logs {
			//		l.BlockHash = block.Hash()
			//	}
			//}
			//for _, log := range work.state.Logs() {
			//	log.BlockHash = block.Hash()
			//}
			// Todo: update local chain tx info.

			stat, err := self.chain.WriteBlockAndState(block, work.receipts, work.state)
			if err != nil {
				log.Error("Failed writing block to chain", "err", err)
				continue
			}
			// check if canon block and write transactions
			if stat == bc.CanonStatTy {
				// implicit by posting ChainHeadEvent
				mustCommitNewWork = false
			}
			// Broadcast the block and announce chain insertion event
			self.mux.Post(bc.NewMinedBlockEvent{Block: block})
			var (
				events []interface{}
				logs   = work.state.Logs()
			)
			events = append(events, bc.ChainEvent{Block: block, Hash: block.Hash(), Logs: logs})
			if stat == bc.CanonStatTy {
				events = append(events, bc.ChainHeadEvent{Block: block})
			}

			self.chain.PostChainEvents(events, logs)

			if mustCommitNewWork {
				// Todo: self.PreMiner()
			}
		}
	}
}

// makeCurrent creates a new environment for the current cycle.
func (self *worker) makeCurrent(parent *types.Block, header *types.Header) error {
	state, err := self.chain.StateAt(parent.Root())
	if err != nil {
		return err
	}
	work := &Work{
		config:    self.config,
		signer:    types.NewQSSigner(self.config.ChainId),
		state:     state,
		header:    header,
		createdAt: time.Now(),
	}

	// Keep track of transactions which return errors so they can be removed
	work.tcount = 0
	self.current = work
	return nil
}

func (self *worker) RoutinePreMine() {
	if p2p.PeerMgrInst().GetLocalType() == discover.BootNode {
		return
	}
	// 1. make header
	// 2. prepare header
	// 3. get tx from txpool
	// 4. commit tx
	// 5. generate and broad proof

	for{
		parent := self.chain.CurrentBlock()

		// make header
		num := parent.Number()
		header := &types.Header{
			ParentHash: parent.Hash(),
			Coinbase:self.coinbase,
			Number:     num.Add(num, common.Big1),
			Extra:      self.extra,
		}
		// prepare header
		pstate, _ := self.chain.StateAt(parent.Root())
		if err := self.engine.PrepareBlockHeader(self.chain, header, pstate); err != nil {
			log.Debug("Failed to prepare header for mining", "err", err)
			return
		}

		// make work
		err := self.makeCurrent(parent, header)
		if err != nil {
			log.Error("Failed to create mining context", "err", err)
			return
		}
		// Create the current work task and check any fork transitions needed

		pending, err := txpool.GetTxPool().Pending(10000)
		if err != nil {
			log.Error("Failed to fetch pending transactions", "err", err)
			return
		}
		txs := types.NewTransactionsByPayload(self.current.signer, pending)

		work := self.current
		work.commitTransactions(self.mux, txs, self.coinbase)

		// generate workproof
		proof, err := self.engine.GenerateProof(self.chain, self.current.header, work.txs)
		if err != nil {
			log.Error("Premine","GenerateProof failed, err", err, "headerNumber", header.Number)
			continue
		}

		// broadcast proof.
		self.mux.Post(bc.WorkProofEvent{Proof:proof})

		// Create the new block to seal with the consensus engine
		if work.Block, err = self.engine.Finalize(self.chain, header, work.state, work.txs, work.receipts); err != nil {
			log.Error("Failed to finalize block for sealing", "err", err)
			return
		}
		//Todo: send to wait confirm.
	}
}

func (self *worker) RoutineVerifyProof() {
	// 1. receive proof
	// 2. verify proof
	// 3. update tx info (signed count)

	// 1. receive proof response
	// 2. calc response count
	// 3. if count > peers/2 , final mined.
}

func (self *worker) RoutineFinalMine() {
	// 1. gen block with proof and header.
	// 2. update txpool.
}

func (env *Work) commitTransactions(mux *sub.TypeMux, txs *types.TransactionsByPayload, coinbase common.Address) {
	for {
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}

		// Start executing the transaction
		env.state.Prepare(tx.Hash(), common.Hash{}, env.tcount)

		err := env.commitTransaction(tx, coinbase)
		switch err {
		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			env.tcount++
			txs.Shift()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}

}

func (env *Work) commitTransaction(tx *types.Transaction, coinbase common.Address) (error) {
	var receipt *types.Receipt
	var err error
	snap := env.state.Snapshot()
	blockchain := bc.InstanceBlockChain()

	receipt, err = bc.ApplyTransaction(env.config, blockchain, &coinbase, env.state, env.header, tx)
	if err != nil {
		env.state.RevertToSnapshot(snap)
		return err
	}

	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return nil
}
