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
	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10
	waitConfirmTimeout = 40 // a proof wait confirm timeout seconds
)
var (
	blockMaxTxs = 5000 * 10
	minTxsToMine = 10000
	blockPeorid = 2
)

// Work is the workers current environment and holds
// all of the current state information
type Work struct {
	config *config.ChainConfig
	state     *state.StateDB 	// apply state changes here
	tcount    int            	// tx count in cycle

	Block *types.Block 			// the new block

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt
	proofs   []*types.ProofState
	createdAt time.Time
	id 			int64
	genCh  	chan error
	confirmed  bool
}

type RoundState byte

const (
	IDLE 		RoundState = iota
	PostMining
	Mining
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
	newRoundCh   chan *types.Header
	exitCh 		 chan struct {}

	chain   *bc.BlockChain
	chainDb shxdb.Database

	coinbase common.Address
	extra    []byte

	currentMu sync.Mutex
	current   *Work
	roundState 	RoundState

	unconfirmed *unconfirmedProofs // set of locally mined blocks pending canonicalness confirmations
	workPending	*WorkPending

	txMu			sync.Mutex
	txConfirmPool 	map[common.Hash]uint64
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
		newRoundCh:  make(chan *types.Header, 1),
		roundState:		IDLE,
		txConfirmPool: make(map[common.Hash]uint64),
		updating:	false,
		history: 	make([]common.Hash,0),
		workPending:NewWorkPending(),
	}

	worker.txpool = txpool.GetTxPool()
	worker.chainHeadSub = bc.InstanceBlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)
	worker.history = append(worker.history, worker.chain.CurrentHeader().ProofHash)
	blockPeorid = int(config.Prometheus.Period)

	// goto listen the event
	go worker.eventListener()
	go worker.workPending.Run()

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

func (self *worker) Setopt(maxtxs,peorid int) {
	blockPeorid = peorid
	blockMaxTxs = maxtxs
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
	self.txMu.Lock()
	defer self.txMu.Unlock()
	self.updating = true
	batch := self.chainDb.NewBatch()
	cnt := 0
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
					// add tx to txpool.
					go txpool.GetTxPool().AddTxs(ev.Proof.Txs)
					// 3. update tx info (tx's signed count)
					self.txMu.Lock()
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
				}()
			}
		}
	}
}

// makeCurrent creates a new environment for the current cycle.
func (self *worker) makeCurrent(parent *types.Header, header *types.Header) error {
	state, err := self.chain.StateAt(parent.Root)
	if err != nil {
		return err
	}
	work := &Work{
		config:    self.config,
		state:     state,
		header:    header,
		proofs:	   make([]*types.ProofState,0),
		createdAt: time.Now(),
		id:			time.Now().UnixNano(),
		genCh: 		make(chan error,1),
		confirmed: false,
	}

	peers := p2p.PeerMgrInst().PeersAll()
	for _, peer := range peers {
		if peer.RemoteType() == discover.MineNode {
			proofState := types.ProofState{Addr:peer.Address(), Root:peer.ProofHash()}
			if root,err := self.engine.GetNodeProof(peer.Address()); err == nil {
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

func (self *worker) CheckNeedStartMine() *types.Header {
	var head *types.Header
	if self.workPending.HaveErr() {
		if self.workPending.Empty() {
			// reset to no error, and continue to mine.
			self.workPending.SetNoError()
		} else {
			return nil
		}
	} 
	if h := self.workPending.Top(); h != nil {
		head = h.Block.Header()
	} else {
		head = self.chain.CurrentHeader()
	}

	now := time.Now().UnixNano()/1000/1000
	pending,_ := self.txpool.Pended()
	delta := now - head.Time.Int64()
	if (delta >= int64(blockPeorid*1000) || (len(pending) >= minTxsToMine) && delta > 20) {
		return head
	}
	return nil
}

func (self *worker) RoutineMine() {
	events := self.mux.Subscribe(bc.ProofConfirmEvent{})
	defer events.Unsubscribe()

	self.confirmCh = make(chan *Work)
	self.newRoundCh = make(chan *types.Header)
	self.unconfirmed = newUnconfirmedProofs(self.confirmCh)
	go self.unconfirmed.RoutineLoop()

	go func() {
		// routine to check new mine round and start new mine round
		evict := time.NewTicker(time.Millisecond * 10)
		defer evict.Stop()
		for {
			select {
			case <-evict.C:
				log.Trace("worker routine check new round")
				if self.roundState == IDLE {
					if h := self.CheckNeedStartMine(); h != nil {
						self.roundState = PostMining
						go func() { self.newRoundCh <- h }()
					}
				} else if self.roundState == Mining {
					// working is mining.
				}
			case lastHeader, ok := <-self.newRoundCh:
				if !ok {
					return
				}
				log.Info("worker routine start new round ", "time ", time.Now().UnixNano()/1000/1000)
				self.roundState = Mining
				self.wg.Add(1)
				go func() {
					defer self.wg.Done()
					if err := self.NewMineRound(lastHeader); err != nil {
						self.roundState = IDLE
					} else {
						self.roundState = Mining
					}
				}()
			}
		}
	}()

	for {
		select {
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
				log.Debug("worker start to exec finalMine", "time ", time.Now().UnixNano()/1000/1000)
				err := self.FinalMine(work)
				if err != nil {
					log.Debug("worker finalmine failed","err ", err)
				}
				self.roundState = IDLE
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

func (self *worker) NewMineRound(parent *types.Header) error {
	if p2p.PeerMgrInst().GetLocalType() == discover.BootNode {
		return nil
	}

	// make header
	if parent == nil {
		parent = self.chain.CurrentHeader()
	}
	num := parent.Number
	header := &types.Header{
		ParentHash: parent.Hash(),
		Coinbase:	self.coinbase,
		Number:     num.Add(num, common.Big1),
		Extra:      self.extra,
	}
	// prepare header
	pstate, _ := self.chain.StateAt(parent.Root)
	if err := self.engine.PrepareBlockHeader(self.chain, header, pstate); err != nil {
		log.Error("Failed to prepare header for mining", "err", err)
		return err
	}
	log.Debug("worker after prepareblock header", "time ", time.Now().UnixNano()/1000/1000)

	// make work
	err := self.makeCurrent(parent, header)
	if err != nil {
		log.Error("Failed to create mining context", "err", err)
		return err
	}

	txs := txpool.GetTxPool().Pending(self.current.id, blockMaxTxs)

	// Create the current work task and check any fork transitions needed
	work := self.current
	work.commitTransactions(txs, self.coinbase)

	log.Info("luxqdebug","total work.txs ", len(work.txs), "total pending txs", len(txs),"time ", time.Now().UnixNano()/1000/1000)
	//for s := int(0); s < len(work.txs); s++ {
	//	tx := work.txs[s]
	//	if tx != nil {
	//		fmt.Printf("luxqdebug txs[%d] : info = %s, receipt = %s\n", s, tx.String(), work.receipts[s].String())
	//	} else {
	//		fmt.Printf("luxqdebug txs[%d] is nil\n", s)
	//	}
	//}

	// generate workproof
	proof, err := self.engine.GenerateProof(self.chain, self.current.header, parent, work.txs, work.proofs)
	if err != nil {
		log.Error("Premine","GenerateProof failed, err", err, "headerNumber", header.Number)
		return err
	}
	log.Debug("SHX profile","generate block proof, blockNumber", header.Number, "proofHash", proof.Signature.Hash(), "time ", time.Now().UnixNano()/1000/1000)

	if config.GetShxConfigInstance().Node.TestMode == 2 {
		// single test, direct pass confirm.
		work.confirmed = true
		go func() {self.confirmCh <- work}()
	} else {
		// broadcast proof.
		self.mux.Post(bc.NewWorkProofEvent{Proof:proof})
		log.Debug("worker proof goto wait confirm","time ", time.Now().UnixNano()/1000/1000)
		// wait confirm.
		self.unconfirmed.Insert(proof, work, consensus.MinerNumber/2 + 1 - 1)
	}
	go func() {
		if block,err := self.engine.Finalize(self.chain, work.header, work.state, work.txs, work.proofs, work.receipts); err != nil {
			work.genCh <- err
		} else {
			log.Debug("worker after engine.Finalize", "time ", time.Now().UnixNano()/1000/1000)
			if result, err := self.engine.GenBlockWithSig(self.chain, block);err != nil {
				work.genCh <- err
			} else {
				log.Debug("worker after engine.GenBlockWithSig", "time ", time.Now().UnixNano()/1000/1000)
				work.Block = result
				work.genCh <- nil
			}
		}
	}()

	return nil
}

func (w *Work) WorkEnded(succeed bool) {
	txpool.GetTxPool().WorkEnded(w.id, w.header.Number.Uint64(), succeed)
}

func (self *worker) FinalMine(work *Work) error {
	// check work confirmed.
	var err error
	defer func() {
		if err != nil {
			go work.WorkEnded(false)
		}
	}()
	if work.confirmed {
		err = <- work.genCh
		if err == nil {
			result := work.Block
			newhist := append(self.history, result.ProofHash())
			if len(newhist) > 10 {
				self.history = make([]common.Hash,10)
				copy(self.history,newhist[len(newhist)-10:])
			}
			if self.workPending.Add(work) {
				log.Info("Successfully sealed new block", "number -> ", result.Number(), "hash -> ", result.Hash(),
					"txs -> ", len(result.Transactions()))
				log.Debug("SHX profile worker", "sealed new block number ", result.Number(), "txs", len(result.Transactions()), "at time", time.Now().UnixNano()/1000/1000)
				return nil
			} else {
				err = errors.New("pending is rollback")
			}
		}
	} else {
		err = errors.New("block proof not confirmed")
	}
	return err
}
