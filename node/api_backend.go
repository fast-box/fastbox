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

// HpbApiBackend implements ethapi.Backend for full nodes
type HpbApiBackend struct {
	hpb *Node
}

func (b *HpbApiBackend) ChainConfig() *config.ChainConfig {
	return &b.hpb.Shxconfig.BlockChain
}

func (b *HpbApiBackend) CurrentBlock() *types.Block {
	return b.hpb.Hpbbc.CurrentBlock()
}

func (b *HpbApiBackend) SetHead(number uint64) {
	b.hpb.Hpbsyncctr.Syncer().Cancel()
	b.hpb.Hpbbc.SetHead(number)
}

func (b *HpbApiBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.hpb.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.hpb.Hpbbc.CurrentBlock().Header(), nil
	}
	return b.hpb.Hpbbc.GetHeaderByNumber(uint64(blockNr)), nil
}

func (b *HpbApiBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.hpb.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.hpb.Hpbbc.CurrentBlock(), nil
	}
	return b.hpb.Hpbbc.GetBlockByNumber(uint64(blockNr)), nil
}

func (b *HpbApiBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
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

func (b *HpbApiBackend) GetBlock(ctx context.Context, blockHash common.Hash) (*types.Block, error) {
	return b.hpb.Hpbbc.GetBlockByHash(blockHash), nil
}

func (b *HpbApiBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	return bc.GetBlockReceipts(b.hpb.ShxDb, blockHash, bc.GetBlockNumber(b.hpb.ShxDb, blockHash)), nil
}

func (b *HpbApiBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.hpb.Hpbbc.GetTdByHash(blockHash)
}

func (b *HpbApiBackend) SubscribeRemovedLogsEvent(ch chan<- bc.RemovedLogsEvent) sub.Subscription {
	return b.hpb.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *HpbApiBackend) SubscribeChainEvent(ch chan<- bc.ChainEvent) sub.Subscription {
	return b.hpb.BlockChain().SubscribeChainEvent(ch)
}

func (b *HpbApiBackend) SubscribeChainHeadEvent(ch chan<- bc.ChainHeadEvent) sub.Subscription {
	return b.hpb.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *HpbApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) sub.Subscription {
	return b.hpb.BlockChain().SubscribeLogsEvent(ch)
}

func (b *HpbApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	log.Debug("SHX profile", "Send tx ", signedTx.Hash(), "at time ", time.Now().UnixNano()/1000/1000)
	return b.hpb.TxPool().AddTx(signedTx)
}

func (b *HpbApiBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.hpb.TxPool().Pended()
	if err != nil {
		return nil, err
	}

	return pending, nil
}

func (b *HpbApiBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.hpb.TxPool().GetTxByHash(hash)
}

func (b *HpbApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.hpb.TxPool().State().GetNonce(addr), nil
}

func (b *HpbApiBackend) Stats() (pending int, queued int) {
	return b.hpb.TxPool().Stats()
}

func (b *HpbApiBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.hpb.TxPool().Content()
}

func (b *HpbApiBackend) SubscribeTxPreEvent(ch chan<- bc.TxPreEvent) sub.Subscription {
	return b.hpb.TxPool().SubscribeTxPreEvent(ch)
}

func (b *HpbApiBackend) Downloader() *synctrl.Syncer {
	return b.hpb.Hpbsyncctr.Syncer()
}

func (b *HpbApiBackend) ProtocolVersion() int {
	return b.hpb.EthVersion()
}

func (b *HpbApiBackend) ChainDb() shxdb.Database {
	return b.hpb.ChainDb()
}

func (b *HpbApiBackend) EventMux() *sub.TypeMux {
	return b.hpb.NewBlockMux()
}

func (b *HpbApiBackend) AccountManager() *accounts.Manager {
	return b.hpb.AccountManager()
}

func (b *HpbApiBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.hpb.bloomIndexer.Sections()
	return config.BloomBitsBlocks, sections
}

func (b *HpbApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.hpb.bloomRequests)
	}
}
