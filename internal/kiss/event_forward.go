// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"context"
	"errors"

	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

// EventBroadcaster accepts receive events from one TNC channel.
type EventBroadcaster interface {
	Broadcast(string, modem.Event)
}

// ForwardEvents relays one TNC channel's receive events to a KISS server until
// cancellation or source shutdown. KISS servers deliberately filter this to
// AX.25 frame events; AIS and EAS remain available to other consumers.
func ForwardEvents(ctx context.Context, channelName string, source *tnc.Channel, destination EventBroadcaster) error {
	if source == nil {
		return errors.New("KISS event source channel is nil")
	}
	if destination == nil {
		return errors.New("KISS event destination is nil")
	}
	events, unsubscribe, err := source.SubscribeEvents(32)
	if err != nil {
		return err
	}
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil
			}
			destination.Broadcast(channelName, event)
		}
	}
}
