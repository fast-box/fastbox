package types

type ProofSignature []byte

type WorkProof struct {
	Signature ProofSignature
	Txs       Transactions
}

type ProofConfirm struct {
	Signature ProofSignature
	Confirm   bool
}
