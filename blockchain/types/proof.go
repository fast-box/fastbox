package types

import "github.com/hpb-project/sphinx/common"

type WorkProof struct {
	ProofHash common.Hash
	txs       Transactions
}
