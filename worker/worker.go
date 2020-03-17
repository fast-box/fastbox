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
	"gopkg.in/fatih/set.v0"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shx-project/sphinx/network/p2p"
	"github.com/shx-project/sphinx/network/p2p/discover"

	"github.com/shx-project/sphinx/blockchain"
	"github.com/shx-project/sphinx/blockchain/state"
	"github.com/shx-project/sphinx/blockchain/storage"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/log"
	"github.com/shx-project/sphinx/config"
	"github.com/shx-project/sphinx/consensus"
	"github.com/shx-project/sphinx/event/sub"
	"github.com/shx-project/sphinx/txpool"
)

const (
	resultQueueSize  = 10
	miningLogAtDepth = 5

	// txChanSize is the size of channel listening to TxPreEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 100000
	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	blockMaxTxs = 8000 * 10
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
	proofs   []*types.ProofState
	createdAt time.Time
	id 			int64
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
	history 		[]common.Hash

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
		history: 	make([]common.Hash,0),
	}

	worker.txpool = txpool.GetTxPool()
	worker.chainHeadSub = bc.InstanceBlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)
	worker.history = append(worker.history, worker.chain.CurrentHeader().ProofHash)

	// goto listen the event
	go worker.eventListener()

	return worker
}

func (self *worker) setShxerbase(addr common.Address) {
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
			self.current.proofs,
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
			self.current.proofs,
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
	log.Debug("worker updateTxConfirm, wait txMux")
	self.txMu.Lock()
	defer self.txMu.Unlock()
	log.Debug("worker updateTxConfirm, got txMux")
	self.updating = true
	batch := self.chainDb.NewBatch()
	cnt := 0
	log.Debug("worker updateTxConfirm, goto updatereceipt")
	for hash,_ := range self.txConfirmPool {
		if receipts, blockHash, blockNumber, err := bc.GetBlockReceiptsByTx(self.chainDb, hash); err == nil {
			for _, receipt := range receipts {
				if confirm,ok := self.txConfirmPool[receipt.TxHash]; ok {
					cnt++
					receipt.ConfirmCount += confirm
					delete(self.txConfirmPool, receipt.TxHash)
				}
			}
			bc.WriteBlockReceipts(batch, blockHash, blockNumber, receipts)
		}
		if cnt > 10000 {
			break
		}
	}
	log.Debug("worker updateTxConfirm, before batch.write")
	if cnt > 0 {
		batch.Write()
	}
	log.Debug("worker updateTxConfirm, after batch.write", "cnt ",cnt)
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
			if !self.updating {
				go self.updateTxConfirm()
			}
			timer.Reset(time.Second)

		case obj := <-events.Chan():
			switch ev:= obj.Data.(type) {
			case bc.WorkProofEvent:
				go func() {
					// 1. receive proof
					// 2. verify proof
					pastLocalRoot := set.New()
					for _,h := range self.history {
						pastLocalRoot.Add(h)
					}


					if atomic.LoadInt32(&self.mining) == 1 {
						if self.current != nil && self.current.header != nil {
							pastLocalRoot.Add(self.current.header.ProofHash)
						}

					}

					if !self.engine.VerifyState(self.coinbase, pastLocalRoot, ev.Proof) {
						var res= types.ProofConfirm{ev.Proof.Signature, false}
						p2p.SendData(ev.Peer, p2p.ProofResMsg, res)
						return
					}

					if err := self.engine.VerifyProof(ev.Peer.Address(), ev.Peer.ProofHash(), ev.Proof, true); err != nil {
						var res= types.ProofConfirm{ev.Proof.Signature, false}
						p2p.SendData(ev.Peer, p2p.ProofResMsg, res)
						return
					} else {
						var res= types.ProofConfirm{ev.Proof.Signature, true}
						p2p.SendData(ev.Peer, p2p.ProofResMsg, res)
					}
					// 3. update tx info (tx's signed count)
					log.Debug("worker get proof, wait txMux to update confirm", "peer", ev.Peer.ID())
					self.txMu.Lock()
					log.Debug("worker get proof, got txMux to update confirm", "peer", ev.Peer.ID())
					for _, tx := range ev.Proof.Txs {
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
					self.txMu.Unlock()
					log.Debug("worker get proof, after update confirm", "peer", ev.Peer.ID())
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
		proofs:	   make([]*types.ProofState,0),
		createdAt: time.Now(),
		id:			time.Now().UnixNano(),
		confirmed: false,
	}

	peers := p2p.PeerMgrInst().PeersAll()
	for _, peer := range peers {
		if peer.RemoteType() == discover.MineNode {
			proofState := types.ProofState{Addr:peer.Address(), Root:peer.ProofHash()}
			if root,err := self.engine.GetNodeProof(peer.Address()); err != nil {
				proofState.Root = root
			}
			work.proofs = append(work.proofs, &proofState)
		}
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
	if (delta >= int64(self.config.Prometheus.Period) || len(pending) >= minTxsToMine) && delta > 0 {
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
			log.Info("worker routine check new round")
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
			log.Info("worker routine start new round")
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

	pending := txpool.GetTxPool().Pending(self.current.id, blockMaxTxs)
	txs := types.NewTransactionsByPayload(self.current.signer, pending)

	work := self.current
	work.commitTransactions(self.mux, txs, self.coinbase)

	// generate workproof
	proof, err := self.engine.GenerateProof(self.chain, self.current.header, work.txs, work.proofs)
	if err != nil {
		log.Error("Premine","GenerateProof failed, err", err, "headerNumber", header.Number)
		return err
	}
	log.Debug("SHX profile","generate block proof, blockNumber", header.Number, "proofHash", proof.Signature.Hash())

	if config.GetShxConfigInstance().Node.TestMode == 2 {
		work.Block, _ = self.engine.Finalize(self.chain, header, work.state, work.txs, work.proofs, work.receipts)
		// single test, direct pass confirm.
		work.confirmed = true
		go func() {self.confirmCh <- work}()
	} else {
		// broadcast proof.
		self.mux.Post(bc.NewWorkProofEvent{Proof:proof})
		log.Info("worker proof goto wait confirm")
		// wait confirm.
		self.unconfirmed.Insert(proof, work, consensus.MinerNumber/2 + 1 - 1)
		work.Block, _ = self.engine.Finalize(self.chain, header, work.state, work.txs, work.proofs, work.receipts)
	}
	log.Info("worker after engine.Finalize")

	return nil
}

func (self *worker) FinalMine(work *Work) error {
	success := false
	defer func() {
		go txpool.GetTxPool().WorkEnded(work.id, work.header.Number.Uint64(), success)
	}()
	if work.confirmed {
		// 1. gen block with proof and header.
		if result, err := self.engine.GenBlockWithSig(self.chain, work.Block); result != nil {
			log.Info("Successfully sealed new block", "number -> ", result.Number(), "hash -> ", result.Hash(),
				"txs -> ", len(result.Transactions()))
			log.Debug("SHX profile", "sealed new block number ", result.Number(), "txs", len(result.Transactions()), "at time", time.Now().UnixNano()/1000/1000)

			newhist := append(self.history, result.ProofHash())
			if len(newhist) > 10 {
				self.history = make([]common.Hash,10)
				copy(self.history,newhist[len(newhist)-10:])
			}

			_, err := self.chain.WriteBlockAndState(result, work.receipts, work.state)
			if err != nil {
				log.Error("Failed writing block to chain", "err", err)
				return err
			}
			log.Info("worker writeBlock and state finished")
			success = true


			// Broadcast the block and announce chain insertion event
			//self.mux.Post(bc.NewMinedBlockEvent{Block: result})
			//var (
			//	events []interface{}
			//	logs   = work.state.Logs()
			//)
			//events = append(events, bc.ChainEvent{Block: result, Hash: result.Hash(), Logs: logs})
			//if stat == bc.CanonStatTy {
			//	// 2. send event and update txpool.
			//	events = append(events, bc.ChainHeadEvent{Block: result})
			//}
			//
			//self.chain.PostChainEvents(events, logs)

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
