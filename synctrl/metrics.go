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
	headerInMeter      = metrics.NewMeter("hpb/syn/headers/in")
	headerReqTimer     = metrics.NewTimer("hpb/syn/headers/req")
	headerDropMeter    = metrics.NewMeter("hpb/syn/headers/drop")
	headerTimeoutMeter = metrics.NewMeter("hpb/syn/headers/timeout")

	bodyInMeter      = metrics.NewMeter("hpb/syn/bodies/in")
	bodyReqTimer     = metrics.NewTimer("hpb/syn/bodies/req")
	bodyDropMeter    = metrics.NewMeter("hpb/syn/bodies/drop")
	bodyTimeoutMeter = metrics.NewMeter("hpb/syn/bodies/timeout")

	receiptInMeter      = metrics.NewMeter("hpb/syn/receipts/in")
	receiptReqTimer     = metrics.NewTimer("hpb/syn/receipts/req")
	receiptDropMeter    = metrics.NewMeter("hpb/syn/receipts/drop")
	receiptTimeoutMeter = metrics.NewMeter("hpb/syn/receipts/timeout")

	stateInMeter   = metrics.NewMeter("hpb/syn/states/in")
	stateDropMeter = metrics.NewMeter("hpb/syn/states/drop")

	//Puller metrics
	propAnnounceInMeter   = metrics.NewMeter("hpb/puller/prop/announces/in")
	propAnnounceOutTimer  = metrics.NewTimer("hpb/puller/prop/announces/out")
	propAnnounceDropMeter = metrics.NewMeter("hpb/puller/prop/announces/drop")
	propAnnounceDOSMeter  = metrics.NewMeter("hpb/puller/prop/announces/dos")

	propBroadcastInMeter   = metrics.NewMeter("hpb/puller/prop/broadcasts/in")
	propBroadcastOutTimer  = metrics.NewTimer("hpb/puller/prop/broadcasts/out")
	propBroadcastDropMeter = metrics.NewMeter("hpb/puller/prop/broadcasts/drop")
	propBroadcastDOSMeter  = metrics.NewMeter("hpb/puller/prop/broadcasts/dos")

	headerFetchMeter = metrics.NewMeter("hpb/puller/fetch/headers")
	bodyFetchMeter   = metrics.NewMeter("hpb/puller/fetch/bodies")

	headerFilterInMeter  = metrics.NewMeter("hpb/puller/filter/headers/in")
	headerFilterOutMeter = metrics.NewMeter("hpb/puller/filter/headers/out")
	bodyFilterInMeter    = metrics.NewMeter("hpb/puller/filter/bodies/in")
	bodyFilterOutMeter   = metrics.NewMeter("hpb/puller/filter/bodies/out")

)
