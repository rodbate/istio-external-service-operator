package v1

import (
	"net"
	"strconv"
)

func (in *IstioExternalServiceEndpoint) String() string {
	if in == nil {
		return ""
	}
	return net.JoinHostPort(in.Host, strconv.Itoa(int(in.Port)))
}
