package worker

import (
	"errors"
	"github.com/hashicorp/golang-lru"
	"github.com/shx-project/sphinx/blockchain"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common/log"
	"github.com/shx-project/sphinx/network/p2p"
	"github.com/shx-project/sphinx/txpool"
	"gopkg.in/fatih/set.v0"
	"sync"
	"sync/atomic"
	"time"
)

type muLru struct {
	mu sync.Mutex
	cache *lru.Cache
}
var handleLocalProof muLru
func init() {
	c3,_ := lru.New(100000)
	handleLocalProof = muLru{cache:c3}
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
	//log.Debug("worker updateTxConfirm, before batch.write")
	if cnt > 0 {
		batch.Write()
	}
	//log.Debug("worker updateTxConfirm, after batch.write", "cnt ",cnt)
	self.updating = false
}

func (self *worker) getBatchProofs(peer *p2p.Peer, start,end uint64,timeout int) error{
	// request BatchProofsData from peer
	var request = types.ReuqestBatchProof{StartNumber:start, EndNumber:end}
	p2p.SendData(peer,p2p.GetProofsMsg, request)
	var peerlock chan struct{}

	self.mu.Lock()
	if l,ok := self.peerLockMap[peer.Address()]; !ok {
		peerlock = make(chan struct{})
		self.peerLockMap[peer.Address()] = peerlock
	} else {
		peerlock = l
	}
	self.mu.Unlock()
	log.Debug("getBatchProofs before <-peerlock", "peer is ", peer.Address())
	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()
	select {
	case <-peerlock:
		log.Debug("getBatchProofs after <-peerlock","peer is ", peer.Address())
		return nil
	case <-timer.C:
		log.Debug("getBatchProofs timeout ","peer is ", peer.Address())
		self.peerLockMap[peer.Address()] = nil
		return errors.New("timeout")
	}

}

func (self *worker) dealProofEvent(event *bc.WorkProofEvent) {
	// 1. receive proof
	// 2. verify proof
	peer := p2p.PeerMgrInst().PeerWithAddr(event.Addr)
	if peer == nil {
		log.Debug("can't deal proof event", "proof miner ", event.Addr.String())
		return
	}

	log.Debug("start deal proof event","peer", peer.ID(), "proof",event.Proof.Number)
	pastLocalRoot := set.New(self.history.Keys())

	if atomic.LoadInt32(&self.mining) == 1 {
		if self.current != nil && self.current.header != nil {
			pastLocalRoot.Add(self.current.header.ProofHash)
		}
	}

	if !self.engine.VerifyState(self.coinbase, pastLocalRoot, event.Proof) {
		log.Debug("worker verify proof state failed")
		var res= types.ProofConfirm{event.Proof.Signature, false}
		self.mux.Post(bc.RoutProofConfirmEvent{Confirm:&res})
		return
	}
	peerProofState := bc.GetPeerProof(self.chainDb, event.Addr)
	if peerProofState == nil {
		if event.Proof.Number == 1 {
			// the first block from peer, lastHash is genesis.ProofHash
			genesis := self.chain.GetHeaderByNumber(0)
			peerProofState = &types.ProofState{Addr:event.Addr, Num:0, Root:genesis.ProofHash}
		} else {
			// request BatchProofsData from peer
			log.Debug("worker goto getBatchProof", "from ", peer.ID(), "st",1,"end",event.Proof.Number - 1)
			if err := self.getBatchProofs(peer, 1, event.Proof.Number - 1, 30); err == nil {
				peerProofState = bc.GetPeerProof(self.chainDb, event.Addr)
			} else {
				var res= types.ProofConfirm{event.Proof.Signature, false}
				self.mux.Post(bc.RoutProofConfirmEvent{Confirm:&res})
				return
			}
		}
	}
	for {
		// loop to request missed proof and re-verify again.
		if newroot, err := self.engine.VerifyProof(event.Addr, peerProofState.Root, event.Proof); err != nil {
			if peerProofState.Num +1 >= event.Proof.Number {
				log.Debug("worker verify proof proofhash failed")
				var res= types.ProofConfirm{event.Proof.Signature, false}
				self.mux.Post(bc.RoutProofConfirmEvent{Confirm:&res})

				return
			} else {

				log.Debug("worker verify proof", "missed proof from", peerProofState.Num + 1, "to",event.Proof.Number-1)
				// request missed proof.
				err := self.getBatchProofs(peer, peerProofState.Num + 1, event.Proof.Number-1, 30)
				if err != nil {
					var res= types.ProofConfirm{event.Proof.Signature, false}
					self.mux.Post(bc.RoutProofConfirmEvent{Confirm:&res})

					return
				}
				peerProofState = bc.GetPeerProof(self.chainDb, event.Addr)
			}
		} else {
			// update peer's proof in local.
			updateProof := types.ProofState{Addr:event.Addr, Num:event.Proof.Number, Root:newroot}
			bc.WritePeerProof(self.chainDb, event.Addr, updateProof)

			var res= types.ProofConfirm{event.Proof.Signature, true}
			self.mux.Post(bc.RoutProofConfirmEvent{Confirm:&res})
			break
		}
	}

	// add tx to txpool.
	go txpool.GetTxPool().AddTxs(event.Proof.Txs)
	// 3. update tx info (tx's signed count)
	self.txMu.Lock()
	for _, tx := range event.Proof.Txs {
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
}

func (self *worker) dealConfirm(ev *bc.ProofConfirmEvent) {
	// 1. receive proof response
	// 2. calc response count
	// 3. if count > peers/2 , final mined.
	handleLocalProof.mu.Lock()
	defer handleLocalProof.mu.Unlock()

	if _,ok := handleLocalProof.cache.Get(ev.Confirm.Signature); ok {
		log.Debug("SHX profile","get confirm for Proof ", ev.Confirm.Signature.Hash(),"from minenode", ev.Addr, "at time ",time.Now().UnixNano()/1000/1000)
		self.unconfirm_mine.Confirm(ev.Addr, ev.Confirm)
	}
}



