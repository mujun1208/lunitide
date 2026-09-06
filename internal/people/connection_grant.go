package people

import "net"

// Each encrypted frame write rechecks the live grant. Revocation cannot retract
// bytes already accepted by the OS, but stops every subsequent payload write.
type peerGrantConn struct {
	net.Conn
	check func() error
}

func (c peerGrantConn) Write(body []byte) (int, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	return c.Conn.Write(body)
}
func (c peerGrantConn) Read(body []byte) (int, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	return c.Conn.Read(body)
}
