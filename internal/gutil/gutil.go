// Copyright 2026 Lucas Saavedra Vaz
// Portions Copyright (c) 2025 DeedleFake, MIT License; see LICENSES/MIT-Trayscale.txt
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package gutil holds GTK helpers adapted from Trayscale's internal/gutil.
package gutil

import (
	"errors"
	"reflect"

	"github.com/diamondburned/gotk4/pkg/core/gerror"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// FillFromBuilder sets fields in the struct dst using objects looked
// up in builder. If the field has a `gtk` tag, the value of it is used
// as the name to look up in builder, unless the tag is exactly `"-"` in
// which case the field is skipped. If it does not have a tag, the field
// name is used instead. If an object by that name does not exist in the
// builder, it is quietly skipped. If it does exist but is the wrong
// type, this function will panic.
func FillFromBuilder[T any](dst *T, builder *gtk.Builder) {
	v := reflect.ValueOf(dst).Elem()
	t := v.Type()

	for i := range t.NumField() {
		fv := v.Field(i)
		ft := t.Field(i)
		if !ft.IsExported() {
			continue
		}

		name := ft.Name
		if tag, ok := ft.Tag.Lookup("gtk"); ok {
			if tag == "-" {
				continue
			}
			name = tag
		}
		obj := builder.GetObject(name)
		if obj == nil {
			continue
		}

		fv.Set(reflect.ValueOf(obj.Cast()))
	}
}

// FillFromUI loads the given xml data in the order that it is passed
// and fills into with it in the same way that [FillFromBuilder] does.
func FillFromUI[T any](into *T, xml ...string) {
	builder := gtk.NewBuilder()
	for _, v := range xml {
		builder.AddFromString(v)
	}

	FillFromBuilder(into, builder)
}

// ErrHasCode returns true if and only if err is a [gerror.GError] and
// its error code is code.
func ErrHasCode(err error, code int) bool {
	var gerr *gerror.GError
	if !errors.As(err, &gerr) {
		return false
	}
	return gerr.ErrorCode() == code
}

// PointerToWidgetter converts a *T that implements gtk.Widgetter to a
// gtk.Widgetter, returning nil if the *T is nil. This avoids the nil
// interface problem.
func PointerToWidgetter[T any, P interface {
	gtk.Widgetter
	*T
}](p P) gtk.Widgetter {
	if p == nil {
		return nil
	}
	return p
}

// Caster wraps the [glib.Object.Cast] method.
type Caster interface {
	Cast() glib.Objector
}

// Assert casts anything that implements Cast to the specified type. Its
// main point is to safely and conveniently handle nil.
func Assert[T any, V any, C interface {
	Caster
	*V
}](obj C) (v T, ok bool) {
	if obj == nil {
		return v, false
	}

	v, ok = obj.Cast().(T)
	return v, ok
}
