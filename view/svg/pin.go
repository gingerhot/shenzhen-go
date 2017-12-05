// Copyright 2017 Google Inc.
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

package main

import (
	"errors"
	"fmt"

	"github.com/google/shenzhen-go/jsutil"
	"github.com/gopherjs/gopherjs/js"
)

const (
	nametagRectStyle = "fill: #efe; fill-opacity: 0.7; stroke: #353; stroke-width:1"
	nametagTextStyle = "font-family:Go; font-size:16; user-select:none; pointer-events:none"
)

// Pin represents a node pin visually, and has enough information to know
// if it is validly connected.
type Pin struct {
	Name, Type string

	input bool     // am I an input?
	node  *Node    // owner.
	ch    *Channel // attached to this channel

	nametag *textBox // Hello, my name is ...

	l    *js.Object // attached line; x1, y1 = x, y; x2, y2 = ch.tx, ch.ty.
	x, y float64    // computed, not relative to node
	circ *js.Object // my main representation
	c    *js.Object // circle, when dragging from a pin
}

func (p *Pin) connectTo(q Point) error {
	switch q := q.(type) {
	case *Pin:
		if p.ch != nil && p.ch != q.ch {
			p.disconnect()
		}
		if q.Type != p.Type {
			return fmt.Errorf("mismatching types [%s != %s]", p.Type, q.Type)
		}
		if q.ch != nil {
			return p.connectTo(q.ch)
		}

		// Prevent mistakes by ensuring that there is at least one input
		// and one output per channel, and they connect separate goroutines.
		if p.input == q.input {
			return errors.New("both pins have the same direction")
		}
		if p.node == q.node {
			return errors.New("both pins are on the same goroutine")
		}

		// Create a new channel to connect to
		ch := newChannel(p.node.d, p, q)
		ch.reposition(nil)
		p.ch, q.ch = ch, ch
		p.node.d.graph.Channels[ch] = struct{}{}
		q.l.Call("setAttribute", "display", "")

	case *Channel:
		if p.ch != nil && p.ch != q {
			p.disconnect()
		}
		if q.Type != p.Type {
			return fmt.Errorf("mismatching types [%s != %s]", p.Type, q.Type)
		}
		same := true
		for r := range q.Pins {
			if r.input != p.input {
				same = false
				break
			}
		}
		if same {
			return errors.New("must connect at least one input and one output")
		}

		// Attach to the existing channel
		p.ch = q
		q.Pins[p] = struct{}{}
		q.reposition(nil)
	}
	return nil
}

func (p *Pin) disconnect() {
	if p.ch == nil {
		return
	}
	delete(p.ch.Pins, p)
	p.ch.setColour(normalColour)
	p.ch.reposition(nil)
	if len(p.ch.Pins) < 2 {
		// Delete the channel
		for q := range p.ch.Pins {
			q.ch = nil
		}
		delete(p.node.d.graph.Channels, p.ch)
	}
	p.ch = nil
}

func (p *Pin) setPos(rx, ry float64) {
	p.circ.Call("setAttribute", "cx", rx)
	p.circ.Call("setAttribute", "cy", ry)
	p.x, p.y = rx+p.node.X, ry+p.node.Y
	if p.l != nil {
		p.l.Call("setAttribute", "x1", p.x)
		p.l.Call("setAttribute", "y1", p.y)
	}
	if p.ch != nil {
		p.ch.reposition(nil)
		p.ch.commit()
	}
}

// Pt returns the diagram coordinate of the pin, for nearest-neighbor purposes.
func (p *Pin) Pt() (x, y float64) { return p.x, p.y }

func (p *Pin) String() string { return fmt.Sprintf("%s.%s", p.node.Name, p.Name) }

func (p *Pin) dragStart(e *js.Object) {
	// If the pin is attached to something, detach and drag from that instead.
	if ch := p.ch; ch != nil {
		p.disconnect()
		p.l.Call("setAttribute", "display", "none")
		if len(ch.Pins) > 1 {
			ch.dragStart(e)
			return
		}
		for q := range ch.Pins {
			q.dragStart(e)
			return
		}
	}
	p.node.d.dragItem = p

	p.circ.Call("setAttribute", "fill", errorColour)

	x, y := p.node.d.cursorPos(e)
	p.l.Call("setAttribute", "x2", x)
	p.l.Call("setAttribute", "y2", y)
	p.c.Call("setAttribute", "cx", x)
	p.c.Call("setAttribute", "cy", y)
	p.c.Call("setAttribute", "stroke", errorColour)
	p.l.Call("setAttribute", "stroke", errorColour)
	p.c.Call("setAttribute", "display", "")
	p.l.Call("setAttribute", "display", "")
}

func (p *Pin) drag(e *js.Object) {
	x, y := p.node.d.cursorPos(e)
	defer func() {
		p.l.Call("setAttribute", "x2", x)
		p.l.Call("setAttribute", "y2", y)
		p.c.Call("setAttribute", "cx", x)
		p.c.Call("setAttribute", "cy", y)
	}()
	d, q := p.node.d.graph.nearestPoint(x, y)

	noSnap := func() {
		if p.ch != nil {
			p.ch.setColour(normalColour)
			p.disconnect()
		}

		p.circ.Call("setAttribute", "fill", errorColour)
		p.l.Call("setAttribute", "stroke", errorColour)
		p.c.Call("setAttribute", "stroke", errorColour)
		p.c.Call("setAttribute", "display", "")
	}

	// Don't connect P to itself, don't connect if nearest is far away.
	if p == q || d >= snapQuad {
		p.node.d.clearError()
		noSnap()
		return
	}

	if err := p.connectTo(q); err != nil {
		p.node.d.setError("Can't connect: "+err.Error(), x, y)
		noSnap()
		return
	}

	// Snap to q.ch, or q if q is a channel. Visual.
	switch q := q.(type) {
	case *Pin:
		x, y = q.ch.tx, q.ch.ty
	case *Channel:
		x, y = q.tx, q.ty
	}

	// Valid snap - ensure the colour is active.
	p.node.d.clearError()
	p.ch.setColour(activeColour)
	p.c.Call("setAttribute", "display", "none")
}

func (p *Pin) drop(e *js.Object) {
	p.node.d.clearError()
	p.circ.Call("setAttribute", "fill", normalColour)
	p.c.Call("setAttribute", "display", "none")
	if p.ch == nil {
		p.l.Call("setAttribute", "display", "none")
		return
	}
	p.ch.setColour(normalColour)
	p.ch.commit()
}

func (p *Pin) mouseEnter(*js.Object) {
	x, y := p.x-p.node.X, p.y-p.node.Y
	if p.input {
		y -= 38
	} else {
		y += 8
	}
	p.nametag.moveTo(x, y)
	p.nametag.show()
}

func (p *Pin) mouseLeave(*js.Object) {
	p.nametag.hide()
}

func (p *Pin) makePinElement(n *Node) *js.Object {
	p.node = n

	p.circ = jsutil.Setup(
		jsutil.MakeSVGElement("circle"),
		map[string]interface{}{
			"r":    pinRadius,
			"fill": normalColour,
		},
		map[string]func(*js.Object){
			"mousedown":  p.dragStart,
			"mouseenter": p.mouseEnter,
			"mouseleave": p.mouseLeave,
		})

	// Line
	p.l = jsutil.Setup(
		jsutil.MakeSVGElement("line"),
		map[string]interface{}{
			"stroke-width": lineWidth,
			"display":      "none",
		}, nil)
	p.node.d.Call("appendChild", p.l)

	// Another circ
	p.c = jsutil.Setup(
		jsutil.MakeSVGElement("circle"),
		map[string]interface{}{
			"r":            pinRadius,
			"fill":         "transparent",
			"stroke-width": lineWidth,
			"display":      "none",
		}, nil)
	p.node.d.Call("appendChild", p.c)

	// Nametag
	p.nametag = newTextBox(p.node.d, fmt.Sprintf("%s (%s)", p.Name, p.Type), nametagTextStyle, nametagRectStyle, 0, 0, 0, 30)
	p.node.box.group.Call("appendChild", p.nametag.group)
	p.nametag.hide()
	return p.circ
}
