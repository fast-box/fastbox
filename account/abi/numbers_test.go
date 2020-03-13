// Copyright 2015 The shx-project Authors
// This file is part of the shx-project library.
//
// The shx-project library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The shx-project library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the shx-project library. If not, see <http://www.gnu.org/licenses/>.

package abi

import (
	"bytes"
	"math/big"
	"testing"
)

func TestNumberTypes(t *testing.T) {
	ubytes := make([]byte, 32)
	ubytes[31] = 1

	unsigned := U256(big.NewInt(1))
	if !bytes.Equal(unsigned, ubytes) {
		t.Errorf("expected %x got %x", ubytes, unsigned)
	}
}
