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
	"sync"
)


type unconfirmedProofs struct {
	proofs 		sync.Map // map[proofhash]int record proof's confirm count.
	works       map[common.Hash]*Work
	confirmedCh chan *Work
}

func newUnconfirmedProofs(confirmedCh chan *Work) *unconfirmedProofs{
	return &unconfirmedProofs{
		works:make(map[common.Hash]*Work),
		confirmedCh:confirmedCh,
	}
}

func (u *unconfirmedProofs) Insert(proof *types.WorkProof, work *Work, threshold int) error {

	return nil
}

func (u *unconfirmedProofs) Confirm(proof *types.WorkProof) error {

	return nil
}
