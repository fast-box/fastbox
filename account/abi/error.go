// Copyright 2016 The shx-project Authors
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
	"errors"
	"fmt"
	"reflect"
)

var (
	errBadBool = errors.New("abi: improperly encoded boolean value")
)

// formatSliceString formats the reflection kind with the given slice size
// and returns a formatted string representation.
func formatSliceString(kind reflect.Kind, sliceSize int) string {
	if sliceSize == -1 {
		return fmt.Sprintf("[]%v", kind)
	}
	return fmt.Sprintf("[%d]%v", sliceSize, kind)
}

// sliceTypeCheck checks that the given slice can by assigned to the reflection
// type in t.
func sliceTypeCheck(t Type, val reflect.Value) error {
	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return typeErr(formatSliceString(t.Kind, t.Size), val.Type())
	}

	if t.T == ArrayTy && val.Len() != t.Size {
		return typeErr(formatSliceString(t.Elem.Kind, t.Size), formatSliceString(val.Type().Elem().Kind(), val.Len()))
	}

	if t.Elem.T == SliceTy {
		if val.Len() > 0 {
			return sliceTypeCheck(*t.Elem, val.Index(0))
		}
	} else if t.Elem.T == ArrayTy {
		return sliceTypeCheck(*t.Elem, val.Index(0))
	}

	if elemKind := val.Type().Elem().Kind(); elemKind != t.Elem.Kind {
		return typeErr(formatSliceString(t.Elem.Kind, t.Size), val.Type())
	}
	return nil
}

// typeCheck checks that the given reflection value can be assigned to the reflection
// type in t.
func typeCheck(t Type, value reflect.Value) error {
	if t.T == SliceTy || t.T == ArrayTy {
		return sliceTypeCheck(t, value)
	}

	// Check base type validity. Element types will be checked later on.
	if t.Kind != value.Kind() {
		return typeErr(t.Kind, value.Kind())
	} else if t.T == FixedBytesTy && t.Size != value.Len() {
		return typeErr(t.Type, value.Type())
	} else {
		return nil
	}

}

// typeErr returns a formatted type casting error.
func typeErr(expected, got interface{}) error {
	return fmt.Errorf("abi: cannot use %v as type %v as argument", got, expected)
}
