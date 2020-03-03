package types

import "github.com/hpb-project/sphinx/common"

type ProofSignature []byte

type WorkProof struct {
	Signature ProofSignature
	Txs       Transactions
}

type ProofConfirm struct {
	Signature ProofSignature
	Confirm   bool
}

func (p ProofSignature)Hash() common.Hash{
	h := common.Hash{}
	h.SetBytes(p)
	return h
}
