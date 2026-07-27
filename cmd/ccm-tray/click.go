package main

// onClick invokes fn once per real click and returns when the channel closes.
//
// This exists because the distinction is a genuine trap. systray.ResetMenu
// closes the ClickedCh of every menu item it removes, so a handler written as
// a bare receive:
//
//	<-item.ClickedCh
//	doSomething()
//
// fires on the close as well as on a click. The Quit handler was written that
// way, which meant the first menu rebuild after any successful switch closed
// the previous generation's Quit channel, unparked that goroutine and quit the
// app. The user saw a success notification followed by the tray disappearing.
//
// Routing every handler through this function makes the close case impossible
// to get wrong again, and gives the behaviour a home that can be tested without
// a display server.
func onClick(ch <-chan struct{}, fn func()) {
	for {
		_, ok := <-ch
		if !ok {
			return
		}
		fn()
	}
}
