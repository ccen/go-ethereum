package p2p

import (
	"database/sql"
	"fmt"
	"math"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/teamnsrg/go-ethereum/log"
	"github.com/teamnsrg/go-ethereum/p2p/discover"
	"math/big"
	"time"
)

func (srv *Server) initSql() error {
	if srv.MySQLName != "" {
		db, err := sql.Open("mysql", srv.MySQLName)
		if err != nil {
			log.Debug("Failed to open sql db handle", "database", srv.MySQLName, "err", err)
			return err
		}
		log.Trace("Opened sql db handle", "database", srv.MySQLName)
		err = db.Ping()
		if err != nil {
			log.Debug("Sql db connection failed ping test", "database", srv.MySQLName, "err", err)
			if err := srv.DB.Close(); err != nil {
				log.Debug("Failed to close sql db handle", "database", srv.MySQLName, "err", err)
			} else {
				log.Trace("Closed sql db handle", "database", srv.MySQLName)
			}
			return err
		}
		log.Trace("Sql db connection passed ping test", "database", srv.MySQLName)
		srv.DB = db

		// fill KnownNodesInfos with info from the mysql database
		srv.loadKnownNodeInfos()

		// prepare sql statements
		srv.prepareAddNodeInfoStmt()
		srv.prepareUpdateNodeInfoStmt()
		srv.prepareAddNodeMetaInfoStmt()
		srv.prepareGetRowID()
	} else {
		log.Trace("No sql db connection info provided")
	}
	return nil
}

func (srv *Server) CloseSql() {
	if srv.DB != nil {
		// close prepared sql statements
		srv.closeSqlStmts()

		// close db handle
		if err := srv.DB.Close(); err != nil {
			log.Debug("Failed to close sql db handle", "database", srv.MySQLName, "err", err)
		} else {
			log.Trace("Closed sql db handle", "database", srv.MySQLName)
		}
	}
}

func (srv *Server) closeSqlStmts() {
	if srv.addNodeInfoStmt != nil {
		if err := srv.addNodeInfoStmt.Close(); err != nil {
			log.Debug("Failed to close AddNodeInfo sql statement", "err", err)
		} else {
			log.Trace("Closed AddNodeInfo sql statement")
		}
	}
	if srv.updateNodeInfoStmt != nil {
		if err := srv.updateNodeInfoStmt.Close(); err != nil {
			log.Debug("Failed to close UpdateNodeInfo sql statement", "err", err)
		} else {
			log.Trace("Closed UpdateNodeInfo sql statement")
		}
	}
	if srv.addNodeMetaInfoStmt != nil {
		if err := srv.addNodeMetaInfoStmt.Close(); err != nil {
			log.Debug("Failed to close AddNodeMetaInfo sql statement", "err", err)
		} else {
			log.Trace("Closed AddNodeMetaInfo sql statement")
		}
	}
	if srv.GetRowIDStmt != nil {
		if err := srv.GetRowIDStmt.Close(); err != nil {
			log.Debug("Failed to close GetRowID sql statement", "err", err)
		} else {
			log.Trace("Closed GetRowID sql statement")
		}
	}
}

func (srv *Server) loadKnownNodeInfos() {
	fields := "ni.id, ni.node_id, nmi.hash, ip, tcp_port, remote_port, " +
		"p2p_version, client_id, caps, listen_port, first_hello_at, last_hello_at, " +
		"protocol_version, network_id, first_received_td, last_received_td, best_hash, genesis_hash, " +
		"first_status_at, last_status_at, dao_fork"
	maxIds := "SELECT node_id as nid, MAX(id) as max_id FROM node_info GROUP BY node_id"
	nodeInfos := fmt.Sprintf("SELECT * FROM node_info x INNER JOIN (%s) max_ids ON x.id = max_ids.max_id", maxIds)
	stmt := fmt.Sprintf("SELECT %s FROM (%s) ni INNER JOIN node_meta_info nmi ON ni.node_id=nmi.node_id", fields, nodeInfos)
	rows, _ := srv.DB.Query(stmt)

	type sqlObjects struct {
		p2pVersion      sql.NullInt64
		clientId        sql.NullString
		caps            sql.NullString
		listenPort      sql.NullInt64
		firstHelloAt    sql.NullFloat64
		lastHelloAt     sql.NullFloat64
		protocolVersion sql.NullInt64
		networkId       sql.NullInt64
		firstReceivedTd sql.NullString
		lastReceivedTd  sql.NullString
		bestHash        sql.NullString
		genesisHash     sql.NullString
		daoForkSupport  sql.NullInt64
		firstStatusAt   sql.NullFloat64
		lastStatusAt    sql.NullFloat64
	}

	srv.KnownNodeInfos.Lock()
	defer srv.KnownNodeInfos.Unlock()

	for rows.Next() {
		var (
			rowid      uint64
			nodeid     string
			hash       string
			ip         string
			tcpPort    uint16
			remotePort uint16
			sqlObj     sqlObjects
		)
		err := rows.Scan(&rowid, &nodeid, &hash, &ip, &tcpPort, &remotePort,
			&sqlObj.p2pVersion, &sqlObj.clientId, &sqlObj.caps, &sqlObj.listenPort,
			&sqlObj.firstHelloAt, &sqlObj.lastHelloAt, &sqlObj.protocolVersion, &sqlObj.networkId,
			&sqlObj.firstReceivedTd, &sqlObj.lastReceivedTd, &sqlObj.bestHash, &sqlObj.genesisHash,
			&sqlObj.daoForkSupport, &sqlObj.firstStatusAt, &sqlObj.lastStatusAt)
		if err != nil {
			log.Debug("Failed to copy values from query result", "err", err)
			continue
		}
		// convert hex to NodeID
		id, err := discover.HexID(nodeid)
		if err != nil {
			log.Debug("Failed to parse node_id value from db", "rowid", rowid, "nodeid", nodeid, "err", err)
			continue
		}
		nodeInfo := &Info{
			RowID:         rowid,
			Keccak256Hash: hash,
			IP:            ip,
			TCPPort:       tcpPort,
			RemotePort:    remotePort,
		}
		if sqlObj.p2pVersion.Valid {
			nodeInfo.P2PVersion = uint64(sqlObj.p2pVersion.Int64)
		}
		if sqlObj.clientId.Valid {
			nodeInfo.ClientId = sqlObj.clientId.String
		}
		if sqlObj.caps.Valid {
			nodeInfo.Caps = sqlObj.caps.String
		}
		if sqlObj.listenPort.Valid {
			nodeInfo.ListenPort = uint16(sqlObj.listenPort.Int64)
		}
		if sqlObj.firstHelloAt.Valid {
			i, f := math.Modf(sqlObj.firstHelloAt.Float64)
			t := time.Unix(int64(i), int64(f*1000000000))
			nodeInfo.FirstHelloAt = &t
		}
		if sqlObj.lastHelloAt.Valid {
			i, f := math.Modf(sqlObj.lastHelloAt.Float64)
			t := time.Unix(int64(i), int64(f*1000000000))
			nodeInfo.LastHelloAt = &t
		}
		if sqlObj.protocolVersion.Valid {
			nodeInfo.ProtocolVersion = uint64(sqlObj.protocolVersion.Int64)
		}
		if sqlObj.networkId.Valid {
			nodeInfo.NetworkId = uint64(sqlObj.networkId.Int64)
		}
		if sqlObj.firstReceivedTd.Valid {
			firstReceivedTd := &big.Int{}
			s := sqlObj.firstReceivedTd.String
			_, ok := firstReceivedTd.SetString(s, 10)
			if !ok {
				log.Debug("Failed to parse first_received_td value from db", "rowid", rowid, "value", s)
			} else {
				nodeInfo.FirstReceivedTd = firstReceivedTd
			}
		}
		if sqlObj.lastReceivedTd.Valid {
			lastReceivedTd := &big.Int{}
			s := sqlObj.lastReceivedTd.String
			_, ok := lastReceivedTd.SetString(s, 10)
			if !ok {
				log.Debug("Failed to parse last_received_td value from db", "rowid", rowid, "value", s)
			} else {
				nodeInfo.LastReceivedTd = lastReceivedTd
			}
		}
		if sqlObj.bestHash.Valid {
			nodeInfo.BestHash = sqlObj.bestHash.String
		}
		if sqlObj.genesisHash.Valid {
			nodeInfo.GenesisHash = sqlObj.genesisHash.String
		}
		if sqlObj.firstStatusAt.Valid {
			i, f := math.Modf(sqlObj.firstStatusAt.Float64)
			t := time.Unix(int64(i), int64(f*1000000000))
			nodeInfo.FirstStatusAt = &t
		}
		if sqlObj.lastStatusAt.Valid {
			i, f := math.Modf(sqlObj.lastStatusAt.Float64)
			t := time.Unix(int64(i), int64(f*1000000000))
			nodeInfo.LastStatusAt = &t
		}
		if sqlObj.daoForkSupport.Valid {
			var daoForkSupport bool
			if uint16(sqlObj.daoForkSupport.Int64) != 0 {
				daoForkSupport = true
			}
			nodeInfo.DAOForkSupport = daoForkSupport
		}
		srv.KnownNodeInfos.Infos()[id] = nodeInfo

		// add the node to the initial static node list
		srv.addInitialStatic(id, nodeInfo)
	}
}

func (srv *Server) prepareAddNodeInfoStmt() {
	fields := []string{"node_id", "ip", "tcp_port", "remote_port", "p2p_version", "client_id", "caps", "listen_port",
		"first_hello_at", "last_hello_at"}

	stmt := fmt.Sprintf(`INSERT INTO node_info (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.Join(fields, ", "))
	pStmt, err := srv.DB.Prepare(stmt)
	if err != nil {
		log.Debug("Failed to prepare AddNodeInfo sql statement", "err", err)
	} else {
		log.Trace("Prepared AddNodeInfo sql statement")
		srv.addNodeInfoStmt = pStmt
	}
}

func (srv *Server) addNodeInfo(newInfoWrapper *KnownNodeInfosWrapper) {
	nodeid := newInfoWrapper.NodeId
	newInfo := newInfoWrapper.Info
	firstUnixTime := float64(newInfo.LastHelloAt.UnixNano()) / 1000000000
	lastUnixTime := float64(newInfo.LastHelloAt.UnixNano()) / 1000000000
	_, err := srv.addNodeInfoStmt.Exec(nodeid, newInfo.IP, newInfo.TCPPort, newInfo.RemotePort,
		newInfo.P2PVersion, newInfo.ClientId, newInfo.Caps, newInfo.ListenPort, firstUnixTime, lastUnixTime)
	if err != nil {
		log.Debug("Failed to execute AddNodeInfo sql statement", "id", nodeid[:16], "newInfo", newInfo, "err", err)
	} else {
		log.Trace("Executed AddNodeInfo sql statement", "id", nodeid[:16], "newInfo", newInfo)
	}
}

func (srv *Server) prepareUpdateNodeInfoStmt() {
	pStmt, err := srv.DB.Prepare("UPDATE node_info SET remote_port=?, last_hello_at=? WHERE id=?")

	if err != nil {
		log.Debug("Failed to prepare UpdateNodeInfo sql statement", "err", err)
	} else {
		log.Trace("Prepared UpdateNodeInfo sql statement")
		srv.updateNodeInfoStmt = pStmt
	}
}

func (srv *Server) updateNodeInfo(newInfoWrapper *KnownNodeInfosWrapper) {
	nodeid := newInfoWrapper.NodeId
	newInfo := newInfoWrapper.Info
	unixTime := float64(newInfo.LastHelloAt.UnixNano()) / 1000000000
	_, err := srv.updateNodeInfoStmt.Exec(newInfo.RemotePort, unixTime, newInfo.RowID)
	if err != nil {
		log.Debug("Failed to execute UpdateNodeInfo sql statement", "id", nodeid[:16], "newInfo", newInfo, "err", err)
	} else {
		log.Trace("Executed UpdateNodeInfo sql statement", "id", nodeid[:16], "newInfo", newInfo)
	}
}

func (srv *Server) prepareAddNodeMetaInfoStmt() {
	var updateFields []string
	fields := []string{"node_id", "hash", "dial_count", "accept_count", "too_many_peers_count"}
	for _, f := range fields[2:] {
		updateFields = append(updateFields, fmt.Sprintf("%s=%s+VALUES(%s)", f, f, f))
	}
	stmt := fmt.Sprintf(`INSERT INTO node_meta_info (%s) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE %s`,
		strings.Join(fields, ", "), strings.Join(updateFields, ", "))
	pStmt, err := srv.DB.Prepare(stmt)
	if err != nil {
		log.Debug("Failed to prepare AddNodeMetaInfo sql statement", "err", err)
	} else {
		log.Trace("Prepared AddNodeMetaInfo sql statement")
		srv.addNodeMetaInfoStmt = pStmt
	}
}

func (srv *Server) addNodeMetaInfo(nodeid string, hash string, dial bool, accept bool, tooManyPeers bool) {
	_, err := srv.addNodeMetaInfoStmt.Exec(nodeid, hash, boolToInt(dial), boolToInt(accept), boolToInt(tooManyPeers))
	if err != nil {
		log.Debug("Failed to execute AddNodeMetaNodeInfo sql statement", "id", nodeid, "dial", dial, "accept", accept, "tooManyPeers", tooManyPeers, "err", err)
	} else {
		log.Trace("Executed AddNodeMetaNodeInfo sql statement", "id", nodeid, "dial", dial, "accept", accept, "tooManyPeers", tooManyPeers)
	}
}

func (srv *Server) prepareGetRowID() {
	pStmt, err := srv.DB.Prepare("SELECT MAX(id) FROM node_info WHERE node_id=?")
	if err != nil {
		log.Debug("Failed to prepare GetRowID sql statement", "err", err)
	} else {
		log.Trace("Prepared GetRowID sql statement")
		srv.GetRowIDStmt = pStmt
	}
}

func (srv *Server) getRowID(nodeid string) uint64 {
	var rowID uint64
	err := srv.GetRowIDStmt.QueryRow(nodeid).Scan(&rowID)
	if err != nil {
		log.Debug("Failed to execute GetRowID sql statement", "id", nodeid, "err", err)
		return 0
	} else {
		log.Trace("Executed GetRowID sql statement", "id", nodeid, "rowid", rowID)
		return rowID
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
