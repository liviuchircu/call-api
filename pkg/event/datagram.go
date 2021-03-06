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
	"net"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/OpenSIPS/call-api/pkg/mi"
	"github.com/OpenSIPS/call-api/internal/jsonrpc"
)

// EventDatagram
type EventDatagramSub struct {
	event string
	conn *EventDatagramConn
	notify EventNotification
	subscribed bool
}

func (sub *EventDatagramSub) String() (string) {
	return sub.conn.String()
}

func (sub *EventDatagramSub) Event() (string) {
	return sub.event
}

func (sub *EventDatagramSub) Unsubscribe() {
	logrus.Debug("unsubscribing event " + sub.event + " from " + sub.conn.String())
	sub.conn.Unsubscribe(sub)
}

func (sub *EventDatagramSub) IsSubscribed() (bool) {
	return sub.subscribed
}

func (sub *EventDatagramSub) setSubscribed() {
	sub.subscribed = true
}

func (sub *EventDatagramSub) setUnsubscribed() {
	sub.subscribed = false
}


// EventDatagramConn
type EventDatagramConn struct {
	udp *net.UDPConn
	wake chan error
	lock sync.RWMutex
	subs []*EventDatagramSub
	handler *EventDatagram
}

func (conn *EventDatagramConn) waitForEvents() {

	buffer := make([]byte, 65535)
	for {
		select {
		case <-conn.wake:
			conn.handler.RemoveConn(conn)
			return
		default:
			r, _, err := conn.udp.ReadFrom(buffer)
			if err == nil {
				result := &jsonrpc.JsonRPCNotification{}
				err = result.Parse(buffer[0:r])
				if err != nil {
					logrus.Error("could not parse notification: " + err.Error())
				} else {
					sub := conn.getSubscription(result.Method)
					// run in a different routine to avoid blocking
					if sub != nil {
						go sub.notify(sub, result)
					} else {
						logrus.Warn("unknown subscriber for event " + result.Method)
					}
				}
			} else {
				logrus.Warn("error while listening for events: " + err.Error())
			}
		}
	}
}

func (conn *EventDatagramConn) Unsubscribe(sub *EventDatagramSub) {
	// first remove it from list, to make sure we don't get any other events
	// for it - locate it in the array
	conn.lock.Lock()
	for i, s := range conn.subs {
		if s == sub {
			conn.subs = append(conn.subs[0:i], conn.subs[i+1:]...)
			break
		}
	}
	if len(conn.subs) == 0 {
		// inform the go routine it is no longer necessary to wait for events
		logrus.Info("closing connection " + conn.String())
		close(conn.wake)
	}
	conn.lock.Unlock()

	if !sub.IsSubscribed() {
		return
	}

	// unsubscribe from the event
	/* we've got the connection - let us subscribe */
	var eviParams = map[string]interface{}{
		"event": sub.Event(),
		"socket": sub.String(),
		"expire": 0,
	}
	_, err := conn.handler.mi.CallSync("event_subscribe", &eviParams);
	if err != nil {
		logrus.Error("could not unsubscribe for event " + sub.Event() + " " + err.Error())
	} else {
		sub.setUnsubscribed()
	}

}

func (conn *EventDatagramConn) Init(event *EventDatagram) (*EventDatagramConn) {

	// we first need to check how we can connect to the MI handler
	miAddr, ok := event.mi.Addr().(*net.UDPAddr)
	if ok != true {
		logrus.Error("using non-UDP protocol to connect to MI")
		return nil
	}
	c, err := net.DialUDP("udp", nil, miAddr)
	if err != nil {
		logrus.Error(err)
		return nil
	}

	udpAddr, ok := c.LocalAddr().(*net.UDPAddr)
	if ok != true {
		logrus.Error("using non-UDP local socket to connect to MI")
		return nil
	}

	// we've now got the IP we can use to reach MI, use it for further events
	local := net.UDPAddr{IP: udpAddr.IP}
	udpConn, err := net.ListenUDP(c.LocalAddr().Network(), &local)
	if err != nil {
		logrus.Error(err)
		return nil
	}
	udpConn.SetReadBuffer(65535)
	udpConn.SetWriteBuffer(65535)

	file, _ := udpConn.File()
	fd := file.Fd()
	syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)

	/* we typically only have one subscriber per conn - lets start with that */
	conn.subs = make([]*EventDatagramSub, 0, 1)
	conn.wake = make(chan error, 1)
	conn.udp = udpConn
	conn.handler = event
	go conn.waitForEvents()
	return conn
}

func (conn *EventDatagramConn) String() (string) {
	return conn.udp.LocalAddr().Network() + ":" + conn.udp.LocalAddr().String()
}

func (conn *EventDatagramConn) getSubscription(event string) (*EventDatagramSub) {
	var subs *EventDatagramSub
	conn.lock.RLock()
	for _, subs = range conn.subs {
		if subs.event == event {
			break
		}
	}
	conn.lock.RUnlock()
	return subs
}


// EventDatagram
type EventDatagram struct {
	mi mi.MI
	lock sync.Mutex
	conns []*EventDatagramConn
}

func (event *EventDatagram) Init(mi mi.MI) (error) {

	/* we typically only use one socket */
	event.conns = make([]*EventDatagramConn, 0, 1)
	event.mi = mi
	return nil
}

func (event *EventDatagram) Subscribe(ev string, notify EventNotification) (Subscription) {

	var conn *EventDatagramConn

	/* search for a connection that does not have this event registered */
	event.lock.Lock()
	for _, conn = range event.conns {
		if conn.getSubscription(ev) == nil {
			break
		}
	}

	if conn == nil {
		conn = &EventDatagramConn{}
		conn.Init(event)
		if conn == nil {
			return nil
		}
		/* add the new connection */
		event.conns = append(event.conns, conn)
	}

	sub := &EventDatagramSub{conn: conn, event:ev, notify: notify}
	conn.subs = append(conn.subs, sub)
	event.lock.Unlock()

	/* we now have a proper conn to listen for events on */
	logrus.Debug("subscribing for " + sub.Event() + " on " + sub.String())

	/* we've got the connection - let us subscribe */
	var eviParams = map[string]interface{}{
		"event": ev,
		"socket": conn.String(),
		"expire": 120,
	}
	_, err := event.mi.CallSync("event_subscribe", &eviParams);
	if err != nil {
		logrus.Error("could not subscribe for event " + ev + ": " + err.Error())
		sub.Unsubscribe()
		return nil
	}
	sub.setSubscribed()

	logrus.Debug("subscribed " + sub.Event() + " at " + sub.String())
	return sub
}

func (event *EventDatagram) Close() {
	logrus.Debug("closing datagram handler")
}

func (event *EventDatagram) RemoveConn(conn *EventDatagramConn) {
	event.lock.Lock()
	for i, c := range event.conns {
		if conn == c {
			logrus.Info("removing connection " + conn.String())
			event.conns = append(event.conns[0:i], event.conns[i+1:]...)
			break;
		}
	}
	event.lock.Unlock()
}
