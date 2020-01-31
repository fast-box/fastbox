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
package prometheus

import (
	"bytes"
	"math/big"
	"time"

	"github.com/hpb-project/sphinx/account"
	"github.com/hpb-project/sphinx/blockchain/state"
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/consensus"

	"errors"
	"github.com/hpb-project/sphinx/common/log"
	"github.com/hpb-project/sphinx/network/rpc"
)

// Prepare function for Block
func (c *Prometheus) PrepareBlockHeader(chain consensus.ChainReader, header *types.Header, state *state.StateDB) error {
	number := header.Number.Uint64()

	parentnum := number - 1
	parentheader := chain.GetHeaderByNumber(parentnum)
	if parentheader == nil {
		return errors.New("-----PrepareBlockHeader parentheader------ is nil")
	}

	// check the header
	if len(header.Extra) < consensus.ExtraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, consensus.ExtraVanity-len(header.Extra))...)
	}

	header.Extra = header.Extra[:consensus.ExtraVanity]

	header.Extra = append(header.Extra, make([]byte, consensus.ExtraSeal)...)

	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}

	header.Time = new(big.Int).Add(parent.Time, new(big.Int).SetUint64(c.config.Period))

	if header.Time.Int64() < time.Now().Unix() {
		header.Time = big.NewInt(time.Now().Unix())
	}

	return nil
}

// generate blocks by giving the signature
func (c *Prometheus) GenBlockWithSig(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	header := block.Header()

	log.Info("HPB Prometheus Seal is starting")

	number := header.Number.Uint64()
	if number == 0 {
		return nil, consensus.ErrUnknownBlock
	}
	// For 0-period chains, refuse to seal empty blocks (no reward but would spin sealing)
	if c.config.Period == 0 && len(block.Transactions()) == 0 {
		return nil, consensus.ErrWaitTransactions
	}

	c.lock.RLock()
	signer, signFn := c.signer, c.signFn

	log.Debug("GenBlockWithSig signer's address", "signer", signer.Hex(), "number", number)

	c.lock.RUnlock()

	delay := time.Unix(header.Time.Int64(), 0).Sub(time.Now())
	if delay < 0 {
		delay = 0
		header.Time = big.NewInt(time.Now().Unix())
	}
	// set delay time for out-turn hpb nodes
	log.Debug("Waiting for slot to sign and propagate", "delay", common.PrettyDuration(delay), "number", number)

	select {
	case <-stop:
		return nil, nil
	case <-time.After(delay):
	}

	header.Coinbase = signer

	// signing to get the signature
	sighash, err := signFn(accounts.Account{Address: signer}, consensus.SigHash(header).Bytes())
	if err != nil {
		return nil, err
	}

	// put the signature result to the Extra field
	copy(header.Extra[len(header.Extra)-consensus.ExtraSeal:], sighash)

	return block.WithSeal(header), nil
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (c *Prometheus) Authorize(signer common.Address, signFn SignerFn) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.signer = signer
	c.signFn = signFn
}

// retrieve the signer from the signature
func (c *Prometheus) Author(header *types.Header) (common.Address, error) {
	return consensus.Ecrecover(header, c.signatures)
}

func (c *Prometheus) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, receipts []*types.Receipt) (*types.Block, error) {
	header.Root = state.IntermediateRoot(true)
	return types.NewBlock(header, txs, receipts), nil
}

// API for the terminal
func (c *Prometheus) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "prometheus",
		Version:   "1.0",
		Service:   &API{chain: chain, prometheus: c},
		Public:    false,
	}}
}

func (c *Prometheus) GetSinger() common.Address {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.signer
}
