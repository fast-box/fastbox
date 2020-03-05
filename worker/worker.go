// Copyright 2018 The sphinx Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The sphinx is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The sphinx is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the sphinx. If not, see <http://www.gnu.org/licenses/>.

package worker

import (
	"errors"
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
	minTxsToMine = 2000

	waitConfirmTimeout = 40 // a proof wait confirm timeout seconds
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
	confirmed  bool
}
type RoundState byte

const (
	IDLE 		RoundState = iota
	Mining
	Finished
	Failed
)

// worker is the main object which takes care of applying messages to the new state
type worker struct {
	config *config.ChainConfig
	engine consensus.Engine
	mu sync.Mutex

	mux          *sub.TypeMux
	wg           sync.WaitGroup
	txpool       *txpool.TxPool
	chainHeadCh  chan bc.ChainHeadEvent
	chainHeadSub sub.Subscription

	confirmCh    chan *Work
	newRoundCh   chan struct {}
	exitCh 		 chan struct {}

	chain   *bc.BlockChain
	chainDb shxdb.Database

	coinbase common.Address
	extra    []byte

	currentMu sync.Mutex
	current   *Work
	roundState 	RoundState

	unconfirmed *unconfirmedProofs // set of locally mined blocks pending canonicalness confirmations

	txMu			sync.Mutex
	txConfirmPool map[common.Hash]uint64
	updating 		bool

	// atomic status counters
	mining int32
}

func newWorker(config *config.ChainConfig, engine consensus.Engine, coinbase common.Address, mux *sub.TypeMux, db shxdb.Database) *worker {

	worker := &worker{
		config:      config,
		engine:      engine,
		mux:         mux,
		chainHeadCh: make(chan bc.ChainHeadEvent, chainHeadChanSize),
		chainDb:     db,
		confirmCh: 	 make(chan *Work, 10),
		chain:       bc.InstanceBlockChain(),
		coinbase:    coinbase,
		exitCh:		 make(chan struct {}),
		newRoundCh:  make(chan struct {}, 1),
		roundState:		IDLE,
		txConfirmPool: make(map[common.Hash]uint64),
		updating:	false,
	}

	worker.txpool = txpool.GetTxPool()
	worker.chainHeadSub = bc.InstanceBlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)

	// goto listen the event
	go worker.eventListener()

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
	if atomic.LoadInt32(&self.mining) == 1 {
		return
	}
	atomic.StoreInt32(&self.mining, 1)
	go self.RoutineMine()
}

func (self *worker) stop() {
	self.mu.Lock()
	defer self.mu.Unlock()
	if atomic.LoadInt32(&self.mining) == 0 {
		return
	}
	self.exitCh <- struct{}{}
	self.wg.Wait()
	atomic.StoreInt32(&self.mining, 0)
}

func (self *worker) updateTxConfirm() {
	self.txMu.Lock()
	defer self.txMu.Unlock()
	self.updating = true
	for hash,confirm := range self.txConfirmPool {
		if receipt, blockHash, blockNumber, index := bc.GetReceipt(self.chainDb, hash); receipt != nil {
			// update receipt
			receipt.ConfirmCount += confirm
			if err := bc.UpdateTxReceiptWithBlock(self.chainDb, hash, blockHash, blockNumber, index, receipt); err != nil {
				log.Debug("worker updateTx receipt", "failed", err)
			} else {
				delete(self.txConfirmPool,hash)
			}
		}
	}
	self.updating = false
}

func (self *worker) eventListener() {
	events := self.mux.Subscribe(bc.WorkProofEvent{})
	defer events.Unsubscribe()

	defer self.chainHeadSub.Unsubscribe()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		// A real event arrived, process interesting content
		select {
		// Handle ChainHeadEvent
		case <-self.chainHeadCh:
			//Todo: self.PreMiner()
		case <-self.chainHeadSub.Err():
			//return

		case <-timer.C:
			timer.Reset(time.Second)
			if !self.updating {
				go self.updateTxConfirm()
			}

		case obj := <-events.Chan():
			switch ev:= obj.Data.(type) {
			case bc.WorkProofEvent:
				go func() {
					// 1. receive proof
					// 2. verify proof
					if err := self.engine.VerifyProof(ev.Peer.Address(), ev.Peer.ProofHash(), ev.Proof, true); err == nil {
						var res= types.ProofConfirm{ev.Proof.Signature, true}
						p2p.SendData(ev.Peer, p2p.ProofResMsg, res)
					}
					// 3. update tx info (tx's signed count)
					self.txMu.Lock()
					for _, tx := range ev.Proof.Txs {
						if receipt, blockHash, blockNumber, index := bc.GetReceipt(self.chainDb, tx.Hash()); receipt != nil {
							// update receipt
							receipt.ConfirmCount += 1
							if err := bc.UpdateTxReceiptWithBlock(self.chainDb, tx.Hash(), blockHash, blockNumber, index, receipt); err != nil {
								log.Error("worker updateTx receipt", "failed", err)
							} else {
								//log.Debug("worker update tx receipt", "hash", tx.Hash(), "count", receipt.ConfirmCount)
							}
						} else {
							// add to unconfirmed tx.
							if v,ok := self.txConfirmPool[tx.Hash()]; ok {
								v += 1
								self.txConfirmPool[tx.Hash()] = v
								//log.Debug("worker update tx map", "hash", tx.Hash(), "count", v)
							} else {
								self.txConfirmPool[tx.Hash()] = 1
								//log.Debug("worker update tx map", "new hash", tx.Hash(), "count", 1)
							}
						}
					}
					self.txMu.Unlock()
				}()
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
		confirmed: false,
	}

	// Keep track of transactions which return errors so they can be removed
	work.tcount = 0
	self.current = work
	return nil
}

func (self *worker) CheckNeedStartMine() bool {
	head := self.chain.CurrentHeader()
	now := time.Now().Unix()
	pending,_ := self.txpool.Pended()
	delta := now - head.Time.Int64()
	//log.Info("CheckNeedStartMine", "Head.tm", head.Time.Int64(), "Now", now, "delta", delta)
	if delta >= int64(self.config.Prometheus.Period) || len(pending) >= minTxsToMine {
		return true
	}
	return false
}

func (self *worker) RoutineMine() {
	events := self.mux.Subscribe(bc.ProofConfirmEvent{})
	defer events.Unsubscribe()
	evict := time.NewTicker(time.Second)
	defer evict.Stop()

	self.confirmCh = make(chan *Work)
	self.newRoundCh = make(chan struct {})
	self.unconfirmed = newUnconfirmedProofs(self.confirmCh)
	go self.unconfirmed.RoutineLoop()
	for {
		select {
		case <-evict.C:
			if self.roundState == Failed {
				go func(){self.newRoundCh <- struct{}{}} ()
			} else if self.roundState == IDLE {
				if self.CheckNeedStartMine() {
					go func(){self.newRoundCh <- struct{}{}} ()
				}
			} else if self.roundState == Mining {
				// working is mining.
			}
		case _, ok := <-self.newRoundCh:
			if !ok {
				return
			}
			self.roundState = Mining
			self.wg.Add(1)
			go func() {
				defer self.wg.Done()
				if err := self.NewMineRound(); err != nil {
					self.roundState = Failed
				} else {
					self.roundState = Mining
				}
			}()
		case obj := <-events.Chan():
			switch ev:= obj.Data.(type) {
			case bc.ProofConfirmEvent:
				// 1. receive proof response
				// 2. calc response count
				// 3. if count > peers/2 , final mined.
				log.Debug("SHX profile","get confirm for Proof ", ev.Confirm.Signature.Hash(),"from peer", ev.Peer.ID(), "at time ",time.Now().UnixNano()/1000/1000)
				self.unconfirmed.Confirm(ev.Peer.Address(), ev.Confirm)
			}
		case work:= <- self.confirmCh:
			self.wg.Add(1)
			go func() {
				defer self.wg.Done()
				if err := self.FinalMine(work); err != nil {
					self.roundState = Failed
				} else {
					self.roundState = IDLE
				}
			}()
		case <-self.exitCh:
			self.unconfirmed.Stop()
			close(self.confirmCh)
			close(self.newRoundCh)
			self.roundState = IDLE
			return
		}
	}
}

func (self *worker) NewMineRound() error {
	if p2p.PeerMgrInst().GetLocalType() == discover.BootNode {
		return nil
	}
	// 1. make header
	// 2. prepare header
	// 3. get tx from txpool
	// 4. commit tx
	// 5. generate and broad proof

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
		log.Error("Failed to prepare header for mining", "err", err)
		return err
	}

	// make work
	err := self.makeCurrent(parent, header)
	if err != nil {
		log.Error("Failed to create mining context", "err", err)
		return err
	}
	// Create the current work task and check any fork transitions needed

	pending := txpool.GetTxPool().Pending(10000)
	txs := types.NewTransactionsByPayload(self.current.signer, pending)

	work := self.current
	work.commitTransactions(self.mux, txs, self.coinbase)

	// generate workproof
	proof, err := self.engine.GenerateProof(self.chain, self.current.header, work.txs)
	if err != nil {
		log.Error("Premine","GenerateProof failed, err", err, "headerNumber", header.Number)
		return err
	}

	// Create the new block to seal with the consensus engine
	if work.Block, err = self.engine.Finalize(self.chain, header, work.state, work.txs, work.receipts); err != nil {
		log.Error("Failed to finalize block for sealing", "err", err)
		return err
	}
	//log.Info("NewRoundMine", "Finalized",true)

	if config.GetHpbConfigInstance().Node.TestMode == 2 {
		// single test, direct pass confirm.
		work.confirmed = true
		go func() {self.confirmCh <- work}()
	} else {
		// broadcast proof.
		self.mux.Post(bc.NewWorkProofEvent{Proof:proof})
		log.Debug("SHX profile","generate block proof, blockNumber", header.Number, "proofHash", proof.Signature.Hash())
		// wait confirm.
		self.unconfirmed.Insert(proof, work, consensus.MinerNumber/2 + 1)
	}

	return nil
}

func (self *worker) FinalMine(work *Work) error {
	if work.confirmed {
		// 1. gen block with proof and header.
		if result, err := self.engine.GenBlockWithSig(self.chain, work.Block); result != nil {
			log.Info("Successfully sealed new block", "number -> ", result.Number(), "hash -> ", result.Hash(),
				"txs -> ", len(result.Transactions()))
			log.Debug("SHX profile", "sealed new block number ", result.Number(), "txs", len(result.Transactions()), "at time", time.Now().UnixNano()/1000/1000)

			stat, err := self.chain.WriteBlockAndState(result, work.receipts, work.state)
			if err != nil {
				log.Error("Failed writing block to chain", "err", err)
				return err
			}

			// Broadcast the block and announce chain insertion event
			self.mux.Post(bc.NewMinedBlockEvent{Block: result})
			var (
				events []interface{}
				logs   = work.state.Logs()
			)
			//log.Debug("WriteBlockAndState", "Stat", stat)
			events = append(events, bc.ChainEvent{Block: result, Hash: result.Hash(), Logs: logs})
			if stat == bc.CanonStatTy {
				// 2. send event and update txpool.
				events = append(events, bc.ChainHeadEvent{Block: result})
			}

			self.chain.PostChainEvents(events, logs)

		} else {
			if err != nil {
				log.Error("Block sealing failed", "err", err)
			}
		}
	} else {
		log.Error("block proof not confirmed")
		return errors.New("proof not confirmed")
	}
	return nil
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
