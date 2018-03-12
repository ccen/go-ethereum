package p2p

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teamnsrg/go-ethereum/crypto"
	"github.com/teamnsrg/go-ethereum/log"
	"github.com/teamnsrg/go-ethereum/p2p/discover"
)

const (
	tdMaxNumDigits = 65
)

type UnixTime struct {
	*time.Time
}

func (t *UnixTime) String() string {
	return strconv.FormatFloat(float64(t.Time.UnixNano())/1000000000, 'f', 6, 64)
}

func (t *UnixTime) Float64() float64 {
	return float64(t.Time.UnixNano()) / 1000000000
}

type Td struct {
	Value    *big.Int
	Overflow bool
}

func NewTd(i *big.Int) *Td {
	var overflow bool
	if len(i.String()) > tdMaxNumDigits {
		overflow = true
	}
	return &Td{Value: i, Overflow: overflow}
}

func (td *Td) String() string {
	if td.Overflow {
		return strings.Repeat("9", tdMaxNumDigits)
	}
	return td.Value.String()
}

// Info represents a short summary of the information known about a known node.
type Info struct {
	mux sync.RWMutex

	RowId         uint64 `json:"rowId"`         // Most recent row ID
	Keccak256Hash string `json:"keccak256Hash"` // Keccak256 hash of node ID
	IP            string `json:"ip"`            // IP address of the node
	TCPPort       uint16 `json:"tcpPort"`       // TCP listening port for RLPx
	RemotePort    uint16 `json:"remotePort"`    // Remote TCP port of the most recent connection

	// DEVp2p Hello info
	P2PVersion   uint64    `json:"p2pVersion,omitempty"`   // DEVp2p protocol version
	ClientId     string    `json:"clientId,omitempty"`     // Name of the node, including client type, version, OS, custom data
	Caps         string    `json:"caps,omitempty"`         // Node's capabilities
	ListenPort   uint16    `json:"listenPort,omitempty"`   // Listening port reported in the node's DEVp2p Hello
	FirstHelloAt *UnixTime `json:"firstHelloAt,omitempty"` // First time the node sent Hello
	LastHelloAt  *UnixTime `json:"lastHelloAt,omitempty"`  // Last time the node sent Hello

	// Ethereum Status info
	ProtocolVersion uint64    `json:"protocolVersion,omitempty"` // Ethereum sub-protocol version
	NetworkId       uint64    `json:"networkId,omitempty"`       // Ethereum network ID
	FirstReceivedTd *Td       `json:"firstReceivedTd,omitempty"` // First reported total difficulty of the node's blockchain
	LastReceivedTd  *Td       `json:"lastReceivedTd,omitempty"`  // Last reported total difficulty of the node's blockchain
	BestHash        string    `json:"bestHash,omitempty"`        // Hex string of SHA3 hash of the node's best owned block
	GenesisHash     string    `json:"genesisHash,omitempty"`     // Hex string of SHA3 hash of the node's genesis block
	FirstStatusAt   *UnixTime `json:"firstStatusAt,omitempty"`   // First time the node sent Status
	LastStatusAt    *UnixTime `json:"lastStatusAt,omitempty"`    // Last time the node sent Status
	DAOForkSupport  int8      `json:"daoForkSupport,omitempty"`  // Whether the node supports or opposes the DAO hard-fork
}

func (k *Info) String() string {
	var s string
	v := reflect.ValueOf(k).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		if len(s) > 0 {
			s += " "
		}
		elem := v.Field(i)
		elemName := t.Field(i).Name
		switch elem.Kind() {
		case reflect.Ptr:
			ptrV := elem.Elem()
			if ptrV.IsValid() {
				s += fmt.Sprintf("%s:%v", elemName, elem.Interface())
			} else {
				s += fmt.Sprintf("%s:nil", elemName)
			}
		case reflect.Struct:
			continue
		default:
			s += fmt.Sprintf("%s:%v", elemName, elem.Interface())
		}
	}
	return s
}

func (k *Info) Address() string {
	return fmt.Sprintf("IP:%v TCPPort:%v RemotePort:%v", k.IP, k.TCPPort, k.RemotePort)
}

func (k *Info) Hello() string {
	return fmt.Sprintf("P2PVersion:%v ClientId:%v Caps:%v ListenPort:%v",
		k.P2PVersion, k.ClientId, k.Caps, k.ListenPort)
}

func (k *Info) Status() string {
	return fmt.Sprintf("ProtocolVersion:%v NetworkId:%v Td:%v BestHash:%v GenesisHash:%v",
		k.ProtocolVersion, k.NetworkId, k.LastReceivedTd, k.BestHash, k.GenesisHash)
}

func (k *Info) P2PSummary() string {
	return fmt.Sprintf("RowID:%v %v %v", k.RowId, k.Address(), k.Hello())
}

func (k *Info) EthSummary() string {
	return fmt.Sprintf("%v %v", k.P2PSummary(), k.Status())
}

func (k *Info) MarshalJSON() ([]byte, error) {
	type Alias Info
	temp := &struct {
		FirstHelloAt    float64 `json:"firstHelloAt,omitempty"`
		LastHelloAt     float64 `json:"lastHelloAt,omitempty"`
		FirstStatusAt   float64 `json:"firstStatusAt,omitempty"`
		LastStatusAt    float64 `json:"lastStatusAt,omitempty"`
		FirstReceivedTd string  `json:"firstReceivedTd,omitempty"`
		LastReceivedTd  string  `json:"lastReceivedTd,omitempty"`
		*Alias
	}{Alias: (*Alias)(k)}
	if k.FirstHelloAt != nil {
		temp.FirstHelloAt = k.FirstHelloAt.Float64()
	}
	if k.LastHelloAt != nil {
		temp.LastHelloAt = k.LastHelloAt.Float64()
	}
	if k.FirstStatusAt != nil {
		temp.FirstStatusAt = k.FirstStatusAt.Float64()
	}
	if k.LastStatusAt != nil {
		temp.LastStatusAt = k.LastStatusAt.Float64()
	}
	if k.FirstReceivedTd != nil {
		temp.FirstReceivedTd = k.FirstReceivedTd.String()
	}
	if k.LastReceivedTd != nil {
		temp.LastReceivedTd = k.LastReceivedTd.String()
	}
	return json.Marshal(temp)
}

func (k *Info) Lock() {
	k.mux.Lock()
}

func (k *Info) Unlock() {
	k.mux.Unlock()
}

func (k *Info) RLock() {
	k.mux.RLock()
}

func (k *Info) RUnlock() {
	k.mux.RUnlock()
}

type knownNodeInfos struct {
	mux   sync.Mutex
	infos map[discover.NodeID]*Info
}

func (k *knownNodeInfos) Lock() {
	k.mux.Lock()
}

func (k *knownNodeInfos) Unlock() {
	k.mux.Unlock()
}

func (k *knownNodeInfos) Infos() map[discover.NodeID]*Info {
	return k.infos
}

func (srv *Server) getNodeAddress(c *conn, receivedAt *time.Time) (*Info, bool, bool) {
	var (
		remoteIP   string
		remotePort uint16
		tcpPort    uint16
		dial       bool
		accept     bool
	)
	addrArr := strings.Split(c.fd.RemoteAddr().String(), ":")
	addrLen := len(addrArr)
	remoteIP = strings.Join(addrArr[:addrLen-1], ":")
	if p, err := strconv.ParseUint(addrArr[addrLen-1], 10, 16); err == nil {
		remotePort = uint16(p)
	}
	srv.KnownNodeInfos.Lock()
	oldNodeInfo := srv.KnownNodeInfos.Infos()[c.id]
	srv.KnownNodeInfos.Unlock()

	var hash string
	if oldNodeInfo != nil {
		oldNodeInfo.RLock()
		hash = oldNodeInfo.Keccak256Hash
		tcpPort = oldNodeInfo.TCPPort
		oldNodeInfo.RUnlock()
	} else {
		hash = crypto.Keccak256Hash(c.id[:]).String()[2:]
	}
	// if inbound connection, resolve the node's listening port
	// otherwise, remotePort is the listening port
	if c.isInbound() {
		if tcpPort == 0 {
			newNode := srv.ntab.Resolve(c.id)
			// if the node address is resolved, set the tcpPort
			// otherwise, leave it as 0
			if newNode != nil {
				tcpPort = newNode.TCP
			}
		}
		accept = true
	} else {
		tcpPort = remotePort
		dial = true
	}
	var unixTime *UnixTime
	if receivedAt != nil {
		unixTime = &UnixTime{Time: receivedAt}
	}
	newNodeInfo := &Info{
		Keccak256Hash: hash,
		FirstHelloAt:  unixTime,
		LastHelloAt:   unixTime,
		IP:            remoteIP,
		TCPPort:       tcpPort,
		RemotePort:    remotePort,
	}
	return newNodeInfo, dial, accept
}

func (srv *Server) storeNodeInfo(c *conn, receivedAt *time.Time, hs *protoHandshake) {
	// node address currentInfo
	newInfo, dial, accept := srv.getNodeAddress(c, receivedAt)
	id := hs.ID
	nodeid := id.String()
	if srv.DB != nil {
		srv.addNodeMetaInfo(nodeid, newInfo.Keccak256Hash, dial, accept, false)
	}

	// DEVp2p Hello
	p2pVersion, clientId, listenPort := hs.Version, hs.Name, uint16(hs.ListenPort)
	var capsArray []string
	for _, c := range hs.Caps {
		capsArray = append(capsArray, c.String())
	}
	sort.Strings(capsArray)
	caps := strings.Join(capsArray, ",")

	// replace unwanted characters
	if srv.strReplacer == nil {
		log.Crit("No strings.Replacer")
		return
	}
	clientId = srv.strReplacer.Replace(clientId)
	caps = srv.strReplacer.Replace(caps)

	newInfo.P2PVersion = p2pVersion
	newInfo.ClientId = clientId
	newInfo.Caps = caps
	newInfo.ListenPort = listenPort

	var infoStr string
	srv.KnownNodeInfos.Lock()
	defer srv.KnownNodeInfos.Unlock()
	if currentInfo, ok := srv.KnownNodeInfos.Infos()[id]; !ok {
		if srv.DB != nil {
			srv.addNodeInfo(&KnownNodeInfosWrapper{nodeid, newInfo})
			if rowId := srv.getRowID(nodeid); rowId > 0 {
				newInfo.RowId = rowId
			}
		}
		infoStr = newInfo.P2PSummary()

		// add the new node as a static node
		srv.addNewStatic(id, newInfo)

		// add new node info to in-memory
		srv.KnownNodeInfos.Infos()[id] = newInfo
	} else {
		if isNewNode(currentInfo, newInfo) {
			// new entry to the mysql db should contain only the new address, DEVp2p info
			// let Ethereum protocol update the Status info, if available.
			if srv.DB != nil {
				srv.addNodeInfo(&KnownNodeInfosWrapper{nodeid, newInfo})
				if rowId := srv.getRowID(nodeid); rowId > 0 {
					newInfo.RowId = rowId
				}
			}
			infoStr = newInfo.P2PSummary()

			// if the node's listening port changed
			// add it as a static node
			if currentInfo.TCPPort != newInfo.TCPPort {
				srv.addNewStatic(id, newInfo)
			}

			// replace the current info with new info, setting all fields related to Ethereum Status to null
			srv.KnownNodeInfos.Infos()[id] = newInfo
		} else {
			currentInfo.Lock()
			defer currentInfo.Unlock()
			currentInfo.LastHelloAt = newInfo.LastHelloAt
			currentInfo.RemotePort = newInfo.RemotePort
			if srv.DB != nil {
				srv.updateNodeInfo(&KnownNodeInfosWrapper{nodeid, currentInfo})
			}
			infoStr = currentInfo.P2PSummary()
		}
	}
	log.Info("[HELLO]", "receivedAt", newInfo.LastHelloAt, "id", nodeid, "addr", c.fd.RemoteAddr().String(), "conn", c.flags, "info", infoStr)
}

func isNewNode(oldInfo *Info, newInfo *Info) bool {
	return oldInfo.IP != newInfo.IP || oldInfo.TCPPort != newInfo.TCPPort || oldInfo.P2PVersion != newInfo.P2PVersion ||
		oldInfo.ClientId != newInfo.ClientId || oldInfo.Caps != newInfo.Caps || oldInfo.ListenPort != newInfo.ListenPort
}

// During the initial node info loading process
// if a node seems to be listening (ie TCPPort != 0)
// add it as a static node
func (srv *Server) addInitialStatic(id discover.NodeID, nodeInfo *Info) {
	if nodeInfo.TCPPort != 0 {
		var ip net.IP
		if ip = net.ParseIP(nodeInfo.IP); ip == nil {
			log.Error("Failed to add node to initial StaticNodes list", "node",
				fmt.Sprintf("enode://%s@%s:%d", id.String(), nodeInfo.IP, nodeInfo.TCPPort),
				"err", "failed to parse ip")
		} else {
			// Ensure the IP is 4 bytes long for IPv4 addresses.
			if ipv4 := ip.To4(); ipv4 != nil {
				ip = ipv4
			}
			log.Debug("Adding node to initial StaticNodes list", "node",
				fmt.Sprintf("enode://%s@%s:%d", id.String(), nodeInfo.IP, nodeInfo.TCPPort))
			srv.StaticNodes = append(srv.StaticNodes, discover.NewNode(id, ip, nodeInfo.TCPPort, nodeInfo.TCPPort))
		}
	}
}

// if a node seems to be listening (ie TCPPort != 0)
// add it as a static node
func (srv *Server) addNewStatic(id discover.NodeID, nodeInfo *Info) {
	if nodeInfo.TCPPort != 0 {
		var ip net.IP
		if ip = net.ParseIP(nodeInfo.IP); ip == nil {
			log.Error("Failed to add static node", "node",
				fmt.Sprintf("enode://%s@%s:%d", id.String(), nodeInfo.IP, nodeInfo.TCPPort),
				"err", "failed to parse ip")
		} else {
			// Ensure the IP is 4 bytes long for IPv4 addresses.
			if ipv4 := ip.To4(); ipv4 != nil {
				ip = ipv4
			}
			srv.AddPeer(discover.NewNode(id, ip, nodeInfo.TCPPort, nodeInfo.TCPPort))
		}
	}
}

type KnownNodeInfosWrapper struct {
	NodeId string `json:"nodeId"` // Unique node identifier (also the encryption key)
	Info   *Info  `json:"info"`
}

// NodeInfo gathers and returns a collection of metadata known about the host.
func (srv *Server) KnownNodes() []*KnownNodeInfosWrapper {
	srv.KnownNodeInfos.Lock()
	defer srv.KnownNodeInfos.Unlock()
	infos := make([]*KnownNodeInfosWrapper, 0, len(srv.KnownNodeInfos.Infos()))
	for id, info := range srv.KnownNodeInfos.Infos() {
		nodeInfo := &KnownNodeInfosWrapper{
			id.String(),
			info,
		}
		infos = append(infos, nodeInfo)
	}
	// Sort the result array alphabetically by node identifier
	for i := 0; i < len(infos); i++ {
		for j := i + 1; j < len(infos); j++ {
			if infos[i].NodeId > infos[j].NodeId {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}
	return infos
}
