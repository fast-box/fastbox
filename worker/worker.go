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
	"math/big"
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
	"gopkg.in/fatih/set.v0"
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

// Agent can register themself with the worker
type Producer interface {
	Work() chan<- *Work
	SetReturnCh(chan<- *Result)
	Stop()
	Start()
}

// Work is the workers current environment and holds
// all of the current state information
type Work struct {
	config *config.ChainConfig
	signer types.Signer

	state     *state.StateDB // apply state changes here
	ancestors *set.Set       // ancestor set (used for checking uncle parent validity)
	family    *set.Set       // family set (used for checking uncle invalidity)
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
	wg           sync.WaitGroup

	producers map[Producer]struct{}
	recv      chan *Result

	chain   *bc.BlockChain
	proc    bc.Validator
	chainDb hpbdb.Database

	coinbase common.Address
	extra    []byte

	currentMu sync.Mutex
	current   *Work

	unconfirmed *unconfirmedBlocks // set of locally mined blocks pending canonicalness confirmations

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
		proc:        bc.InstanceBlockChain().Validator(),
		coinbase:    coinbase,
		producers:   make(map[Producer]struct{}),
		unconfirmed: newUnconfirmedBlocks(bc.InstanceBlockChain(), miningLogAtDepth),
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

	// spin up agents
	for producer := range self.producers {
		producer.Start()
	}
}

func (self *worker) stop() {
	self.wg.Wait()

	self.mu.Lock()
	defer self.mu.Unlock()
	if atomic.LoadInt32(&self.mining) == 1 {
		for producer := range self.producers {
			producer.Stop()
		}
	}
	atomic.StoreInt32(&self.mining, 0)
	atomic.StoreInt32(&self.atWork, 0)
}

func (self *worker) register(producer Producer) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.producers[producer] = struct{}{}
	producer.SetReturnCh(self.recv)
}

func (self *worker) unregister(producer Producer) {
	self.mu.Lock()
	defer self.mu.Unlock()
	delete(self.producers, producer)
	producer.Stop()
}

func (self *worker) eventListener() {

	//defer self.txSub.Unsubscribe()
	defer self.chainHeadSub.Unsubscribe()

	for {
		// A real event arrived, process interesting content
		select {
		// Handle ChainHeadEvent
		case <-self.chainHeadCh:
			self.startNewMinerRound()

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

			// Update the block hash in all logs since it is now available and not when the
			// receipt/log of individual transactions were created.
			for _, r := range work.receipts {
				for _, l := range r.Logs {
					l.BlockHash = block.Hash()
				}
			}
			for _, log := range work.state.Logs() {
				log.BlockHash = block.Hash()
			}
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

			// Insert the block into the set of pending ones to wait for confirmations
			self.unconfirmed.Insert(block.NumberU64(), block.Hash())

			if mustCommitNewWork {
				self.startNewMinerRound()
			}
		}
	}
}

// push sends a new work task to currently live miner agents.
func (self *worker) push(work *Work) {
	if atomic.LoadInt32(&self.mining) != 1 {
		return
	}
	for producer := range self.producers {
		atomic.AddInt32(&self.atWork, 1)
		if ch := producer.Work(); ch != nil {
			ch <- work
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
		ancestors: set.New(),
		family:    set.New(),
		header:    header,
		createdAt: time.Now(),
	}

	// when 08 is processed ancestors contain 07 (quick block)
	for _, ancestor := range self.chain.GetBlocksFromHash(parent.Hash(), 7) {
		for _, uncle := range ancestor.Uncles() {
			work.family.Add(uncle.Hash())
		}
		work.family.Add(ancestor.Hash())
		work.ancestors.Add(ancestor.Hash())
	}

	// Keep track of transactions which return errors so they can be removed
	work.tcount = 0
	self.current = work
	return nil
}

func (self *worker) calMaxTxs(parent *types.Block) int {
	return blockMaxTxs
}

func (self *worker) PreMiner() {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.currentMu.Lock()
	defer self.currentMu.Unlock()

	tstart := time.Now()
	parent := self.chain.CurrentBlock()
	tstamp := tstart.Unix()

	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		Extra:      self.extra,
		Time:       big.NewInt(tstamp),
	}
	// Only set the coinbase if we are mining (avoid spurious block rewards)
	if atomic.LoadInt32(&self.mining) == 1 {
		header.Coinbase = self.coinbase
	}
	pstate, _ := self.chain.StateAt(parent.Root())

	if err := self.engine.PrepareBlockHeader(self.chain, header, pstate); err != nil {
		log.Debug("Failed to prepare header for mining", "err", err)
		return
	}

	err := self.makeCurrent(parent, header)
	if err != nil {
		log.Error("Failed to create mining context", "err", err)
		return
	}
	if p2p.PeerMgrInst().GetLocalType() == discover.BootNode {
		return
	}
	// Create the current work task and check any fork transitions needed
	work := self.current
	pending, err := txpool.GetTxPool().Pending(10000)
	if err != nil {
		log.Error("Failed to fetch pending transactions", "err", err)
		return
	}
	txs := types.NewTransactionsByPayload(self.current.signer, pending)
	maxtxs := self.calMaxTxs(parent)
	work.commitTransactions(self.mux, txs, self.coinbase, maxtxs)
	log.Debug("worker startNewMinerRound after commitTransactions", "time", time.Now().Unix())

	// Create the new block to seal with the consensus engine
	if work.Block, err = self.engine.Finalize(self.chain, header, work.state, work.txs, work.receipts); err != nil {
		log.Error("Failed to finalize block for sealing", "err", err)
		return
	}
	// We only care about logging if we're actually mining.
	if atomic.LoadInt32(&self.mining) == 1 {
		log.Info("Commit new mining work", "number", work.Block.Number(), "txs", work.tcount, "elapsed", common.PrettyDuration(time.Since(tstart)))
		self.unconfirmed.Shift(work.Block.NumberU64() - 1)
	}
	self.push(work)
}

func (self *worker) startNewMinerRound() {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.currentMu.Lock()
	defer self.currentMu.Unlock()

	tstart := time.Now()
	parent := self.chain.CurrentBlock()

	log.Debug("worker startNewMinerRound", "time", tstart.Unix())

	tstamp := tstart.Unix()
	if parent.Time().Cmp(new(big.Int).SetInt64(tstamp)) >= 0 {
		tstamp = parent.Time().Int64() + 1
	}
	// this will ensure we're not going off too far in the future
	if now := time.Now().Unix(); tstamp > now+1 {
		wait := time.Duration(tstamp-now) * time.Second
		log.Info("Mining too far in the future", "wait", common.PrettyDuration(wait))
		time.Sleep(wait)
	}

	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		Extra:      self.extra,
		Time:       big.NewInt(tstamp),
	}
	// Only set the coinbase if we are mining (avoid spurious block rewards)
	if atomic.LoadInt32(&self.mining) == 1 {
		header.Coinbase = self.coinbase
	}
	pstate, _ := self.chain.StateAt(parent.Root())
	log.Debug("worker startNewMinerRound before PrepareBlockHeader", "number", header.Number.Uint64(), "time", time.Now().Unix())
	if err := self.engine.PrepareBlockHeader(self.chain, header, pstate); err != nil {
		log.Debug("Failed to prepare header for mining", "err", err)
		return
	}

	err := self.makeCurrent(parent, header)
	if err != nil {
		log.Error("Failed to create mining context", "err", err)
		return
	}
	if p2p.PeerMgrInst().GetLocalType() == discover.BootNode {
		return
	}
	// Create the current work task and check any fork transitions needed
	work := self.current
	pending, err := txpool.GetTxPool().Pending(10000)
	if err != nil {
		log.Error("Failed to fetch pending transactions", "err", err)
		return
	}
	txs := types.NewTransactionsByPayload(self.current.signer, pending)
	maxtxs := self.calMaxTxs(parent)
	work.commitTransactions(self.mux, txs, self.coinbase, maxtxs)
	log.Debug("worker startNewMinerRound after commitTransactions", "time", time.Now().Unix())

	// Create the new block to seal with the consensus engine
	if work.Block, err = self.engine.Finalize(self.chain, header, work.state, work.txs, work.receipts); err != nil {
		log.Error("Failed to finalize block for sealing", "err", err)
		return
	}
	// We only care about logging if we're actually mining.
	if atomic.LoadInt32(&self.mining) == 1 {
		log.Info("Commit new mining work", "number", work.Block.Number(), "txs", work.tcount, "elapsed", common.PrettyDuration(time.Since(tstart)))
		self.unconfirmed.Shift(work.Block.NumberU64() - 1)
	}
	self.push(work)
}

func (env *Work) commitTransactions(mux *sub.TypeMux, txs *types.TransactionsByPayload, coinbase common.Address, maxTxs int) {
	var coalescedLogs []*types.Log
	var capTxs int
	if maxTxs > blockMaxTxs || maxTxs < 0 {
		capTxs = blockMaxTxs
	} else {
		capTxs = maxTxs
	}

	for {
		// Retrieve the next transaction and abort if all done
		if len(env.txs) >= capTxs {
			break
		}
		tx := txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(env.signer, tx)

		// Start executing the transaction
		env.state.Prepare(tx.Hash(), common.Hash{}, env.tcount)

		err, logs := env.commitTransaction(tx, coinbase)
		switch err {
		case bc.ErrGasLimitReached:
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			txs.Shift()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}

	if len(coalescedLogs) > 0 || env.tcount > 0 {
		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		go func(logs []*types.Log, tcount int) {
			if len(logs) > 0 {
				mux.Post(bc.PendingLogsEvent{Logs: logs})
			}
			if tcount > 0 {
				mux.Post(bc.PendingStateEvent{})
			}
		}(cpy, env.tcount)
	}
}

func (env *Work) commitTransaction(tx *types.Transaction, coinbase common.Address) (error, []*types.Log) {
	var receipt *types.Receipt
	var err error
	snap := env.state.Snapshot()
	blockchain := bc.InstanceBlockChain()

	//Todo: change to new ApplyTransaction for sphinx
	receipt, _, err = bc.ApplyTransactionNonContract(env.config, blockchain, &coinbase, env.state, env.header, tx)
	if err != nil {
		env.state.RevertToSnapshot(snap)
		return err, nil
	}

	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return nil, receipt.Logs
}
