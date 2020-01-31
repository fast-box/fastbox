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

package txpool

import (
	"errors"
	"fmt"
	"github.com/hpb-project/sphinx/blockchain"
	"github.com/hpb-project/sphinx/blockchain/state"
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/common/log"
	"github.com/hpb-project/sphinx/config"
	"github.com/hpb-project/sphinx/event"
	"github.com/hpb-project/sphinx/event/sub"
	"sync"
	"sync/atomic"
	"time"
)

var (
	evictionInterval     = time.Minute     // Time interval to check for evictable transactions
	statsReportInterval  = 5 * time.Second // Time interval to report transaction pool stats
	chanHeadBuffer       = 10
	maxTransactionSize   = common.StorageSize(32 * 1024)
	tmpQEvictionInterval = 3 * time.Minute // Time interval to check for evictable tmpQueue transactions
)

var INSTANCE = atomic.Value{}
var STOPPED = atomic.Value{}

// blockChain provides the state of blockchain and current gas limit to do
// some pre checks in tx pool.
type blockChain interface {
	CurrentBlock() *types.Block
	GetBlock(hash common.Hash, number uint64) *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)

	SubscribeChainHeadEvent(ch chan<- bc.ChainHeadEvent) sub.Subscription
}

type TxPool struct {
	wg           sync.WaitGroup
	dealwg       sync.WaitGroup
	stopCh       chan struct{}
	chain        blockChain
	chainHeadSub sub.Subscription
	chainHeadCh  chan bc.ChainHeadEvent

	fullCh      chan *types.Transaction
	verifyCh    chan *types.Transaction
	pendingCh   chan *types.Transaction
	invalidTxCh chan *types.Transaction

	txFeed sub.Feed
	scope  sub.SubscriptionScope

	txPreTrigger *event.Trigger
	signer       types.Signer
	config       config.TxPoolConfiguration

	queue          sync.Map //map[txhash]*types.Transaction
	pending        sync.Map //map[txhash]*types.Transaction
	onChain        sync.Map //map[txhash]uint64				tx has been inserted to chain
	poolBlockCount uint64   //block count pooled in onChain.

	smu sync.RWMutex // mutex for below.

	currentState *state.StateDB      // Current state in the blockchain head
	pendingState *state.ManagedState // Pending state tracking virtual nonces
}

//Create the transaction pool and start main process loop.
func NewTxPool(config config.TxPoolConfiguration, chainConfig *config.ChainConfig, blockChain blockChain) *TxPool {
	if INSTANCE.Load() != nil {
		return INSTANCE.Load().(*TxPool)
	}
	//2.Create the transaction pool with its initial settings
	pool := &TxPool{
		config:         config,
		chain:          blockChain,
		signer:         types.NewQSSigner(chainConfig.ChainId),
		chainHeadCh:    make(chan bc.ChainHeadEvent, chanHeadBuffer),
		stopCh:         make(chan struct{}),
		fullCh:         make(chan *types.Transaction, 1000000),
		verifyCh:       make(chan *types.Transaction, 100000),
		invalidTxCh:    make(chan *types.Transaction, 100000),
		pendingCh:      make(chan *types.Transaction, 100000),
		poolBlockCount: 100,
	}
	INSTANCE.Store(pool)
	return pool
}

func (pool *TxPool) Start() {

	pool.chainHeadSub = pool.chain.SubscribeChainHeadEvent(pool.chainHeadCh)

	// Register Publish TxPre publisher
	pool.txPreTrigger = event.RegisterTrigger("tx_pool_tx_pre_publisher")

	// start main process loop
	pool.wg.Add(1)
	go pool.loop()
}

func GetTxPool() *TxPool {
	if INSTANCE.Load() != nil {
		return INSTANCE.Load().(*TxPool)
	}
	log.Warn("TxPool is nil, please init tx pool first.")
	return nil
}

//Stop the transaction pool.
func (pool *TxPool) Stop() {
	if STOPPED.Load() == nil {
		//1.stop main process loop
		pool.stopCh <- struct{}{}
		//2.wait quit
		pool.wg.Wait()
		STOPPED.Store(true)
	}
}

//Main process loop.
func (pool *TxPool) loop() {
	defer pool.wg.Done()

	// Start the stats reporting and transaction eviction tickers
	evict := time.NewTicker(evictionInterval)
	defer evict.Stop()

	// Keep waiting for and reacting to the various events
	for {
		select {
		// Handle ChainHeadEvent
		case ev := <-pool.chainHeadCh:
			if ev.Block != nil {
				pool.JustPending(ev.Block)
			}
		// Handle onChain tx over block count.
		case <-evict.C:
			pool.FitOnChain()

		//stop signal
		case <-pool.stopCh:
			close(pool.fullCh)
			close(pool.verifyCh)
			pool.dealwg.Wait()
			return
		}
	}
}

func (pool *TxPool) FitOnChain() {
	head := pool.chain.CurrentBlock()
	curHigh := head.Number().Uint64()
	pool.onChain.Range(func(k, v interface{}) bool {
		number := v.(uint64)
		if number > curHigh+pool.poolBlockCount {
			pool.onChain.Delete(k)
		}
		return true
	})
}

func (pool *TxPool) validateTx(tx *types.Transaction) error {
	// Heuristic limit, reject transactions over 32KB to prevent DOS attacks
	if tx.Size() > maxTransactionSize {
		log.Trace("ErrOversizedData maxTransactionSize", "ErrOversizedData", ErrOversizedData)
		return ErrOversizedData
	}
	return nil
}

func (pool *TxPool) dupTx(tx *types.Transaction) error {
	hash := tx.Hash()
	if _, ok := pool.queue.Load(hash); ok {
		log.Trace("Discarding already known transaction", "hash", hash)
		return fmt.Errorf("known transaction: %x", hash)
	}
	if _, ok := pool.pending.Load(hash); ok {
		log.Trace("Discarding already known transaction", "hash", hash)
		return fmt.Errorf("known transaction: %x", hash)
	}
	if _, ok := pool.onChain.Load(hash); ok {
		log.Trace("Discarding already known transaction", "hash", hash)
		return fmt.Errorf("known transaction: %x", hash)
	}
	return nil
}

func (pool *TxPool) verifyTx(tx *types.Transaction) bool {
	if _, err := types.Sender(pool.signer, tx); err != nil {
		log.Error("verifyTx Sender ErrInvalidSender", "tx.hash", tx.Hash())
		return false
	}
	return true
}

func (pool *TxPool) sendToVerify(tx *types.Transaction) error {
	select {
	case pool.verifyCh <- tx:
		log.Trace("tx pool send to verify", "tx.Hash", tx.Hash())
	default:
		return errors.New("tx pool verifyCh is full")
	}
	return nil
}

func (pool *TxPool) DealTxRoutine() {
	pool.dealwg.Add(1)
	go func() {
		defer pool.dealwg.Done()
		for {
			select {
			case tx, ok := <-pool.fullCh:
				if !ok {
					//channel closed.
					return
				}
				pool.queue.Store(tx.Hash(), tx)
				pool.sendToVerify(tx)
			}
		}
	}()

	pool.dealwg.Add(1)
	go func() {
		// VerifyRoutine
		defer pool.dealwg.Done()
		for {
			select {
			case tx, ok := <-pool.verifyCh:
				if !ok {
					//channel closed.
					return
				}
				if pool.verifyTx(tx) {
					pool.pending.Store(tx.Hash(), tx)
					pool.queue.Delete(tx.Hash())
					pool.pendingCh <- tx                   // wait miner sign
					go pool.txFeed.Send(bc.TxPreEvent{tx}) // send to route tx.
				} else {
					pool.invalidTxCh <- tx
				}
			}
		}
	}()
}

func (pool *TxPool) AddTx(tx *types.Transaction) error {
	if err := pool.validateTx(tx); err != nil {
		return err
	}
	if dup := pool.dupTx(tx); dup != nil {
		return dup
	}
	select {
	case pool.fullCh <- tx:
		log.Trace("AddTx", "tx.Hash", tx.Hash())
	default:
		return errors.New("tx pool is full")
	}
	return nil
}

func (pool *TxPool) AddTxs(txs []*types.Transaction) error {
	for i, tx := range txs {
		err := pool.AddTx(tx)
		if err != nil {
			log.Debug("AddTxs to add tx failed", "index", i, "err", err)
			return err
		}
	}
	return nil
}

// reset move package in block transactions from pending to onChain.
func (pool *TxPool) JustPending(newblock *types.Block) {
	newChainTxs := newblock.Transactions()
	for _, tx := range newChainTxs {
		pool.onChain.Store(tx.Hash(), tx)
		pool.pending.Delete(tx.Hash())
	}
}

//For RPC

// stats retrieves the current pool stats, namely the number of pending and the
// number of queued (non-executable) transactions.
func (pool *TxPool) Stats() (int, int) {
	pending := 0
	queued := 0
	pool.pending.Range(func(k, v interface{}) bool {
		pending += 1
		return true
	})
	pool.queue.Range(func(k, v interface{}) bool {
		queued += 1
		return true
	})

	return pending, queued
}

// Get returns a transaction if it is contained in the pool
// and nil otherwise.
func (pool *TxPool) GetTxByHash(hash common.Hash) *types.Transaction {
	v, ok := pool.pending.Load(hash)
	if !ok {
		v, ok = pool.queue.Load(hash)
		if !ok {
			log.Trace("not Finding already known tmptx transaction", "hash", hash)
			return nil
		}
		tmptx := v.(*types.Transaction)
		return tmptx
	}
	tx := v.(*types.Transaction)
	return tx

}

// Pending retrieves all currently processable transactions, groupped by origin
// account and sorted by nonce. The returned transaction set is a copy and can be
// freely modified by calling code.
func (pool *TxPool) Pending(maxtxs int) (pending types.Transactions, err error) {
	for i := 0; i < maxtxs; {
		select {
		case tx := <-pool.pendingCh:
			pending = append(pending, tx)
		default:
			return
		}
	}
	return
}

func (pool *TxPool) Pended() (pending types.Transactions, err error) {
	pool.pending.Range(func(k, v interface{}) bool {
		tx := v.(*types.Transaction)
		pending = append(pending, tx)
		return true
	})
	return
}

// State returns the virtual managed state of the transaction pool.
func (pool *TxPool) State() *state.ManagedState {
	pool.smu.RLock()
	defer pool.smu.RUnlock()

	return pool.pendingState
}

func (pool *TxPool) Content() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	pending := make(map[common.Address]types.Transactions)
	queued := make(map[common.Address]types.Transactions)
	return pending, queued
}

func (pool *TxPool) SubscribeTxPreEvent(ch chan<- bc.TxPreEvent) sub.Subscription {
	return pool.scope.Track(pool.txFeed.Subscribe(ch))
}
