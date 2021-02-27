// Copyright 2018 The sphinx Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The sphinx is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The sphinx is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the sphinx. If not, see <http://www.gnu.org/licenses/>.

// Contains the metrics collected by the syn.

package synctrl

import (
	"github.com/shx-project/sphinx/common/metrics"
)

var (
	headerInMeter      = metrics.NewMeter("shx/syn/headers/in")
	headerReqTimer     = metrics.NewTimer("shx/syn/headers/req")
	headerDropMeter    = metrics.NewMeter("shx/syn/headers/drop")
	headerTimeoutMeter = metrics.NewMeter("shx/syn/headers/timeout")

	bodyInMeter      = metrics.NewMeter("shx/syn/bodies/in")
	bodyReqTimer     = metrics.NewTimer("shx/syn/bodies/req")
	bodyDropMeter    = metrics.NewMeter("shx/syn/bodies/drop")
	bodyTimeoutMeter = metrics.NewMeter("shx/syn/bodies/timeout")

	receiptInMeter      = metrics.NewMeter("shx/syn/receipts/in")
	receiptReqTimer     = metrics.NewTimer("shx/syn/receipts/req")
	receiptDropMeter    = metrics.NewMeter("shx/syn/receipts/drop")
	receiptTimeoutMeter = metrics.NewMeter("shx/syn/receipts/timeout")

	stateInMeter   = metrics.NewMeter("shx/syn/states/in")
	stateDropMeter = metrics.NewMeter("shx/syn/states/drop")

	//Puller metrics
	propAnnounceInMeter   = metrics.NewMeter("shx/puller/prop/announces/in")
	propAnnounceOutTimer  = metrics.NewTimer("shx/puller/prop/announces/out")
	propAnnounceDropMeter = metrics.NewMeter("shx/puller/prop/announces/drop")
	propAnnounceDOSMeter  = metrics.NewMeter("shx/puller/prop/announces/dos")

	propBroadcastInMeter   = metrics.NewMeter("shx/puller/prop/broadcasts/in")
	propBroadcastOutTimer  = metrics.NewTimer("shx/puller/prop/broadcasts/out")
	propBroadcastDropMeter = metrics.NewMeter("shx/puller/prop/broadcasts/drop")
	propBroadcastDOSMeter  = metrics.NewMeter("shx/puller/prop/broadcasts/dos")

	headerFetchMeter = metrics.NewMeter("shx/puller/fetch/headers")
	bodyFetchMeter   = metrics.NewMeter("shx/puller/fetch/bodies")

	headerFilterInMeter  = metrics.NewMeter("shx/puller/filter/headers/in")
	headerFilterOutMeter = metrics.NewMeter("shx/puller/filter/headers/out")
	bodyFilterInMeter    = metrics.NewMeter("shx/puller/filter/bodies/in")
	bodyFilterOutMeter   = metrics.NewMeter("shx/puller/filter/bodies/out")
)
