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

package prometheus

import (
	//"fmt"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/consensus"
	"github.com/shx-project/sphinx/network/rpc"
)

type API struct {
	chain      consensus.ChainReader
	prometheus *Prometheus
}

func (api *API) GetLatestBlockHeader(number *rpc.BlockNumber) (header *types.Header) {
	if number == nil || *number == rpc.LatestBlockNumber {
		header = api.chain.CurrentHeader()
	} else {
		header = api.chain.GetHeaderByNumber(uint64(number.Int64()))
	}
	return header
}

func (api *API) Proposals() map[common.Address]bool {
	api.prometheus.lock.RLock()
	defer api.prometheus.lock.RUnlock()

	proposals := make(map[common.Address]bool)
	for address, auth := range api.prometheus.proposals {
		proposals[address] = auth
	}
	return proposals
}

func (api *API) Propose(address common.Address, confRand string, auth bool) {
	api.prometheus.lock.Lock()
	defer api.prometheus.lock.Unlock()
	api.prometheus.proposals[address] = auth
}

func (api *API) Discard(address common.Address, confRand string) {
	api.prometheus.lock.Lock()
	defer api.prometheus.lock.Unlock()
	delete(api.prometheus.proposals, address)
}
