package prometheus

import (
	"github.com/hpb-project/sphinx/common"
	"time"
)

func (c *Prometheus) UpdateProof(addr common.Address, hash common.Hash) {
	if val, ok := c.proofs.Load(addr); ok {
		if pf, ok := val.(*PeerProof); ok {
			pf.RootHash = hash
			pf.Latest = time.Now().Unix()
		}
	} else {
		pf := &PeerProof{time.Now().Unix(), hash}
		c.proofs.Store(addr, pf)
	}
}
