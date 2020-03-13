package types

import (
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/rlp"
)

type ProofState struct {
	Addr 	common.Address
	Root 	common.Hash
}

type ProofStates []*ProofState

// Len returns the length of s
func (s ProofStates) Len() int { return len(s) }

// Swap swaps the i'th and the j'th element in s
func (s ProofStates) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// GetRlp implements Rlpable and returns the i'th element of s in rlp
func (s ProofStates) GetRlp(i int) []byte {
	enc, _ := rlp.EncodeToBytes(s[i])
	return enc
}


type ProofSignature []byte

type WorkProof struct {
	Signature ProofSignature
	Txs       Transactions
	States 	  ProofStates
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

