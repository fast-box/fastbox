package types

import (
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/merkletree"
	"github.com/shx-project/sphinx/common/rlp"
)

type ProofState struct {
	Addr 	common.Address
	Root 	common.Hash
	Num     uint64
}

func (p ProofState)CalculateHash()([]byte, error){
	ret := p.Root.Bytes()
	return ret,nil
}

func (p ProofState)Equals(other merkletree.Content)(bool, error){
	op := other.(ProofState)
	if p.Root == op.Root && p.Addr == op.Addr {
		return true,nil
	}
	return false, nil
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

func (s ProofStates)GetMerkleContent(i int) merkletree.Content{
	return s[i]
}

type ProofSignature []byte
func (p ProofSignature)Hash() common.Hash{
	h := common.Hash{}
	h.SetBytes(p)
	return h
}

type WorkProof struct {
	Number 	  uint64
	Signature ProofSignature
	Txs       Transactions
	States 	  ProofStates
}

type ProofConfirm struct {
	Signature ProofSignature
	Confirm   bool
}

type ReuqestBatchProof struct {
	StartNumber uint64
	EndNumber uint64
}

type ResponseProofData struct {
	Number uint64
	TxRoot common.Hash
	ProofHash common.Hash
}
type BatchProofData []ResponseProofData


