//
// Copyright (C) 2020 OpenSIPS Solutions
//
// Call API is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Call API is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//

package event

import (
	"github.com/sirupsen/logrus"
	"github.com/OpenSIPS/call-api/pkg/mi"
	"github.com/OpenSIPS/call-api/internal/jsonrpc"
)

type EventNotification func(sub Subscription, notify *jsonrpc.JsonRPCNotification)

type Subscription interface {
	Event() (string)
	String() (string)
	Unsubscribe()
	IsSubscribed() (bool)
}

type Event interface {
	Init(mi.MI) (error)
	Close()
	Subscribe(event string, notify EventNotification) (Subscription)
}

func EventHandler(mi mi.MI) (Event) {
	/* TODO: check based on config what exactly to do here */
	event := new(EventDatagram)
	if err := event.Init(mi); err != nil {
		logrus.Printf("ERROR creating: %v", err)
		return nil
	}
	return event
}
