// Command devderp runs a local DERP relay for trying Katzensprung without
// the public tailcat relays (or without internet access):
//
//	go run ./tools/devderp
//	KATZENSPRUNG_DERPMAP_URL=http://127.0.0.1:8475/derpmap.json ./katzensprung
//
// It is for development only: the relay uses a self-signed certificate that
// clients accept without verification.
package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httptest"

	"tailscale.com/derp/derpserver"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/logger"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8475", "address for the DERP map")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal(err)
	}

	d := derpserver.New(key.NewNode(), logger.Discard)
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		log.Fatal(err)
	}
	relay := httptest.NewUnstartedServer(derpserver.Handler(d))
	relay.Listener.Close()
	relay.Listener = ln
	relay.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	relay.StartTLS()

	dm := &tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
		900: {
			RegionID:   900,
			RegionCode: "local",
			RegionName: "Lokales Test-Relay",
			Nodes: []*tailcfg.DERPNode{{
				Name:             "900a",
				RegionID:         900,
				HostName:         host,
				IPv4:             host,
				IPv6:             "none",
				STUNPort:         -1,
				DERPPort:         ln.Addr().(*net.TCPAddr).Port,
				InsecureForTests: true,
			}},
		},
	}}
	http.HandleFunc("/derpmap.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dm)
	})
	log.Printf("DERP relay on %v; DERP map at http://%s/derpmap.json", ln.Addr(), *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
