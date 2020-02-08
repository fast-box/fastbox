// Copyright 2016 The go-hbp Authors
// This file is part of the go-hbp library.
//
// The go-hbp library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-hbp library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-hbp library. If not, see <http://www.gnu.org/licenses/>.

package worker

import (
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"gopkg.in/fatih/set.v0"
	"sync"
	"time"
)

type proofInfo struct {
	threshold int
	work  *Work
	confirmed *set.Set
	time  int64
}

type unconfirmedProofs struct {
	// proof --> proofInfo
	proofs 		sync.Map // map[signature]proofInfo record proof's info.
	confirmedCh chan *Work
	stopCh      chan struct{}
}

func newUnconfirmedProofs(confirmedCh chan *Work) *unconfirmedProofs{
	return &unconfirmedProofs{
		confirmedCh:confirmedCh,
		stopCh:make(chan struct{}),
	}
}

func (u *unconfirmedProofs) Insert(proof *types.WorkProof, work *Work, threshold int) error {
	if _, ok := u.proofs.Load(proof.Signature); !ok {
		info := &proofInfo{threshold:threshold, work:work, confirmed:set.New(), time:time.Now().Unix()}
		u.proofs.Store(proof.Signature, info)
		return nil
	}
	return nil
}

func (u *unconfirmedProofs) Confirm(addr common.Address, confirm *types.ProofConfirm) error {
	if v, ok := u.proofs.Load(confirm.Signature); ok {
		info := v.(*proofInfo)
		if confirm.Confirm == true {
			info.confirmed.Add(addr)
		}
		if info.confirmed.Size() >= info.threshold {
			// send to worker.
			info.work.confirmed = true
			u.confirmedCh <- info.work
			u.proofs.Delete(confirm.Signature)
		}
	}
	return nil
}

func (u *unconfirmedProofs) Stop() {
	u.stopCh <- struct{}{}
}


func (u *unconfirmedProofs) RoutineLoop () {
	evict := time.NewTicker(10 * time.Second)
	defer evict.Stop()
	for {
		select {
		case <-evict.C:
			u.proofs.Range(func(k, v interface{}) bool {
				info := v.(*proofInfo)
				now := time.Now().Unix()
				time.Now().Sub(info.work.createdAt)
				if now - info.time > waitConfirmTimeout {
					// unconfirmed proof, drop work.
					u.confirmedCh <- info.work
					u.proofs.Delete(k)
				}
				return true
			})
		case <-u.stopCh:
			return
		}
	}
}