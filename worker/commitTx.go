package worker

import (
	"github.com/shx-project/sphinx/blockchain"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/log"
)

func (env *Work) commitTransactions(txs *types.TransactionsByPayload, coinbase common.Address) {
	for {
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}

		// Start executing the transaction
		env.state.Prepare(tx.Hash(), common.Hash{}, env.tcount)

		err := env.commitTransaction(tx, coinbase)
		switch err {
		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			env.tcount++
			txs.Shift()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}

}

func (env *Work) commitTransaction(tx *types.Transaction, coinbase common.Address) (error) {
	var receipt *types.Receipt
	var err error
	snap := env.state.Snapshot()
	blockchain := bc.InstanceBlockChain()

	receipt, err = bc.ApplyTransaction(env.config, blockchain, &coinbase, env.state, env.header, tx)
	if err != nil {
		env.state.RevertToSnapshot(snap)
		return err
	}

	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return nil
}

