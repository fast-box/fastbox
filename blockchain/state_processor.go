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

package bc

import (
	"errors"
	"math/big"

	"github.com/hpb-project/sphinx/blockchain/state"
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/config"
	"github.com/hpb-project/sphinx/consensus"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *config.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *config.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Hpb rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB) (types.Receipts, []*types.Log, error) {
	var (
		receipts types.Receipts
		receipt  *types.Receipt
		errs     error
		header   = block.Header()
		allLogs  []*types.Log
	)
	synsigner := types.MakeSigner(p.config)
	go func(txs types.Transactions) {
		for _, tx := range txs {
			types.ASynSender(synsigner, tx)
		}
	}(block.Transactions())

	// Iterate over and process the individual transactions
	author, _ := p.engine.Author(block.Header())
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)

		// Todo: chang to new ApplyTransaction for sphinx
		receipt, _, errs = ApplyTransactionNonContract(p.config, p.bc, &author, statedb, header, tx)
		if errs != nil {
			return nil, nil, errs
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	if _, errfinalize := p.engine.Finalize(p.bc, header, statedb, block.Transactions(), receipts); nil != errfinalize {
		return nil, nil, errfinalize
	}

	return receipts, allLogs, nil
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransactionNonContract(config *config.ChainConfig, bc *BlockChain, author *common.Address, statedb *state.StateDB, header *types.Header, tx *types.Transaction) (*types.Receipt, *big.Int, error) {
	return nil, nil, errors.New("need change to new ApplyTransaction for sphinx")
}
