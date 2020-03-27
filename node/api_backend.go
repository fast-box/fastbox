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

package node

import (
	"context"
	"github.com/shx-project/sphinx/common/log"
	"math/big"
	"time"

	"github.com/shx-project/sphinx/account"
	"github.com/shx-project/sphinx/blockchain"
	"github.com/shx-project/sphinx/blockchain/bloombits"
	"github.com/shx-project/sphinx/blockchain/state"
	"github.com/shx-project/sphinx/blockchain/storage"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/config"
	"github.com/shx-project/sphinx/event/sub"
	"github.com/shx-project/sphinx/network/rpc"
	"github.com/shx-project/sphinx/synctrl"
)

// ShxApiBackend implements ethapi.Backend for full nodes
type ShxApiBackend struct {
	hpb *Node
}

func (b *ShxApiBackend) ChainConfig() *config.ChainConfig {
	return &b.hpb.Shxconfig.BlockChain
}

func (b *ShxApiBackend) CurrentBlock() *types.Block {
	return b.hpb.Shxbc.CurrentBlock()
}

func (b *ShxApiBackend) SetHead(number uint64) {
	b.hpb.Shxsyncctr.Syncer().Cancel()
	b.hpb.Shxbc.SetHead(number)
}

func (b *ShxApiBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.hpb.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.hpb.Shxbc.CurrentBlock().Header(), nil
	}
	return b.hpb.Shxbc.GetHeaderByNumber(uint64(blockNr)), nil
}

func (b *ShxApiBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.hpb.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.hpb.Shxbc.CurrentBlock(), nil
	}
	return b.hpb.Shxbc.GetBlockByNumber(uint64(blockNr)), nil
}

func (b *ShxApiBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block, state := b.hpb.miner.Pending()
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, nil, err
	}
	stateDb, err := b.hpb.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *ShxApiBackend) GetBlock(ctx context.Context, blockHash common.Hash) (*types.Block, error) {
	return b.hpb.Shxbc.GetBlockByHash(blockHash), nil
}

func (b *ShxApiBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	return bc.GetBlockReceipts(b.hpb.ShxDb, blockHash, bc.GetBlockNumber(b.hpb.ShxDb, blockHash)), nil
}

func (b *ShxApiBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.hpb.Shxbc.GetTdByHash(blockHash)
}

func (b *ShxApiBackend) SubscribeRemovedLogsEvent(ch chan<- bc.RemovedLogsEvent) sub.Subscription {
	return b.hpb.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *ShxApiBackend) SubscribeChainEvent(ch chan<- bc.ChainEvent) sub.Subscription {
	return b.hpb.BlockChain().SubscribeChainEvent(ch)
}

func (b *ShxApiBackend) SubscribeChainHeadEvent(ch chan<- bc.ChainHeadEvent) sub.Subscription {
	return b.hpb.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *ShxApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) sub.Subscription {
	return b.hpb.BlockChain().SubscribeLogsEvent(ch)
}

func (b *ShxApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	log.Debug("SHX profile", "Send tx ", signedTx.Hash(), "at time ", time.Now().UnixNano()/1000/1000)
	return b.hpb.TxPool().AddTx(signedTx)
}

func (b *ShxApiBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.hpb.TxPool().Pended()
	if err != nil {
		return nil, err
	}

	return pending, nil
}

func (b *ShxApiBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.hpb.TxPool().GetTxByHash(hash)
}

func (b *ShxApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.hpb.TxPool().State().GetNonce(addr), nil
}

func (b *ShxApiBackend) Stats() (pending int, queued int) {
	return b.hpb.TxPool().Stats()
}

func (b *ShxApiBackend) TxPoolContent() (types.Transactions, types.Transactions) {
	return b.hpb.TxPool().Content()
}

func (b *ShxApiBackend) SubscribeTxPreEvent(ch chan<- bc.TxPreEvent) sub.Subscription {
	return b.hpb.TxPool().SubscribeTxPreEvent(ch)
}

func (b *ShxApiBackend) Downloader() *synctrl.Syncer {
	return b.hpb.Shxsyncctr.Syncer()
}

func (b *ShxApiBackend) ProtocolVersion() int {
	return b.hpb.EthVersion()
}

func (b *ShxApiBackend) ChainDb() shxdb.Database {
	return b.hpb.ChainDb()
}

func (b *ShxApiBackend) EventMux() *sub.TypeMux {
	return b.hpb.NewBlockMux()
}

func (b *ShxApiBackend) AccountManager() *accounts.Manager {
	return b.hpb.AccountManager()
}

func (b *ShxApiBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.hpb.bloomIndexer.Sections()
	return config.BloomBitsBlocks, sections
}

func (b *ShxApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.hpb.bloomRequests)
	}
}
