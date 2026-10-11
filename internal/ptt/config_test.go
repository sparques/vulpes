// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"testing"

	"github.com/sparques/vulpes/internal/config"
)

func TestOpenConfiguredNoop(t *testing.T) {
	keyer, err := OpenConfigured(context.Background(), config.PTT{Type: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if err := keyer.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := keyer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
