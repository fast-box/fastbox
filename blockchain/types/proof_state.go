package types

import (
	"bytes"
	"encoding/binary"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/crypto/sha3"
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
	hash := sha3.Sum256(p)
	h.SetBytes(hash[:])
	return h
}


type WorkProofMsg struct {
	Proof WorkProof
	Sign []byte
}

type ConfirmMsg struct {
	Confirm ProofConfirm
	Sign []byte
}

type QueryStateMsg struct {
	Qs QueryState
	Sign []byte
}

type ResponseStateMsg struct {
	Rs ResponseState
	Sign []byte
}

type WorkProof struct {
	Number 	  uint64
	Sign 	  ProofSignature
	Txs       Transactions
	States 	  ProofStates
}

// return data to sign a signature.
func (wp WorkProof)Data() []byte {
	buf := bytes.NewBuffer([]byte{})
	tmpbuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tmpbuf, wp.Number)
	buf.Write(tmpbuf)
	buf.Write(wp.Sign)
	return buf.Bytes()
}

type ProofConfirm struct {
	Signature ProofSignature
	Confirm   bool
}

func (pc ProofConfirm)Data() []byte {
	buf := bytes.NewBuffer([]byte{})
	buf.Write(pc.Signature)
	if pc.Confirm {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	return buf.Bytes()
}

type QueryState struct {
	Miner common.Address
	Number uint64
}

func (qs QueryState)Data() []byte {
	buf := bytes.NewBuffer([]byte{})
	tmpbuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tmpbuf, qs.Number)
	buf.Write(tmpbuf)
	buf.Write(qs.Miner.Bytes())
	return buf.Bytes()
}

type ResponseState struct {
	Number uint64
	Root common.Hash
	Querier common.Address
}

func (rs ResponseState )Data() []byte {
	buf := bytes.NewBuffer([]byte{})
	tmpbuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tmpbuf, rs.Number)
	buf.Write(tmpbuf)
	buf.Write(rs.Root.Bytes())
	buf.Write(rs.Querier.Bytes())
	return buf.Bytes()
}
