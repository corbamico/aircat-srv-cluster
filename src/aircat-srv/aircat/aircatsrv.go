package aircat

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
)

const (
	logInfo    = 1
	logWarning = 3
	logError   = 5
	logDebug   = 7
)

//Server run at port 9000
type Server struct {
	w                influxdb
	controlChan      chan controlMessage //used for send json/string from restSrv to aircatDevice.listenControl()
	restSrv          *restServer         //
	connCache        connCache           //all connections link to device, store {mac->net.Conn}
	connClusterCache connClusterCache    //redis store {mac->restsrv-addr}
}

type controlMessage struct {
	mac  string
	json string
}

//NewAircatServer create a AircatServer
func NewAircatServer() Server {
	if configs.InitDelay > 0 && configs.InitDelay < 60 {
		time.Sleep(time.Duration(configs.InitDelay) * time.Second)
	}

	controlChan := make(chan controlMessage)
	var connClusterCache connClusterCache
	if configs.ClusterMode {
		connClusterCache = newConnClusterCache()
	}
	return Server{
		w:                newInfluxdb(configs.InfluxdbServer),
		controlChan:      controlChan,
		restSrv:          newrestServer(controlChan),
		connCache:        newConnCache(),
		connClusterCache: connClusterCache,
	}
}

//Run AircatServer at port 9000
func (s *Server) Run() {
	listen, err := net.Listen("tcp", configs.ServerAddr)
	if err != nil {
		glog.Fatalln(err)
	}
	glog.Infof("Server run at %s\n", listen.Addr().String())
	//run REST Server in backend go-routine
	s.restSrv.setAircatServer(s)
	s.restSrv.Run()

	//run recieve controlmsg in backend go-routine
	go s.listenControl()

	for {
		conn, err := listen.Accept()

		if err != nil {
			glog.Infoln(err)
			continue
		}
		glog.Infof("Cient connected at %s\n", conn.RemoteAddr().String())
		device := newAircatDevice(s, conn)
		go device.run()
	}
}

func (s *Server) listenControl() {
	for {
		select {
		case ctrlmsg, ok := <-s.controlChan:
			if !ok { //chan closed.
				return
			}
			//1. handle mac in local memory
			err := s.handleControl(ctrlmsg)

			if !configs.ClusterMode {
				continue
			}
			//2. handle mac in redis cache
			if errKeyIncorrect == err {
				s.redirectControl(ctrlmsg)
			}
		}
	}
}

type keyIncorrectError int

func (ki keyIncorrectError) Error() string {
	return "aircat: key not in cache"
}

var errKeyIncorrect error = keyIncorrectError(0)

func (s *Server) handleControl(ctrlmsg controlMessage) error {
	// 1. query mac connection in local memory
	cached, err := s.connCache.get(ctrlmsg.mac)
	// 1.1 query mac connection in local memory, error
	if err != nil {
		glog.V(logDebug).Infof("query mac in hashmap, error = %s\n", err.Error())
		return err
	}
	// 1.2 query mac connection in local memory, success
	glog.V(logDebug).Infof("query mac in hashmap, value=%v\n", cached)
	if cached != nil {
		//if hit local cache
		msg := cached.(itemCache).msg
		bytes := msg.controlMsg(ctrlmsg.json)
		cached.(itemCache).conn.Write(bytes)
		return nil
	}
	return errKeyIncorrect
}
func (s *Server) redirectControl(ctrlmsg controlMessage) error {
	return s.connClusterCache.redirectJSON(ctrlmsg.mac, ctrlmsg.json)
}

type aircatDevice struct {
	sever *Server
	conn  net.Conn
}

func newAircatDevice(sever *Server, conn net.Conn) *aircatDevice {
	return &aircatDevice{sever: sever, conn: conn}
}

func (client *aircatDevice) run() {
	cached := false
	macFirst := ""
	buf := make([]byte, 10240)
	for {
		nRead, err := client.conn.Read(buf)
		var msg message
		if err != nil {
			glog.Infof("Cient disconnected at %s, with error (%s)\n", client.conn.RemoteAddr().String(), err)
			break
		}
		if nRead < sizeMinMessage || nRead > sizeMaxMessage {
			continue
		}
		if err = msg.readMsg(buf[0:nRead]); err != nil {
			continue
		}
		//we got right packet
		//then we write to influxdb
		if msg.header.MsgType == 4 {
			//client.msg = msg
		}
		mac := hex.EncodeToString(msg.header.Mac[1:7])

		if !cached {
			client.sever.connCache.insert(mac, itemCache{client.conn, msg})
			glog.V(logDebug).Infof("insert connCache (mac=%s)\n", mac)

			addr := client.conn.LocalAddr().String()
			addr = strings.TrimSuffix(addr, ":9000")

			glog.V(logDebug).Infof("insert connClusterCache (mac=%s),(addr=%s)\n", mac, addr)
			client.sever.connClusterCache.insertL2Cached(mac, addr)
			macFirst = mac
			cached = true
		}

		if len(msg.json) > 0 {
			client.sever.w.write(mac, msg.json)
		}
	}

	if len(macFirst) > 0 {
		client.sever.connCache.delete(macFirst)
	}
	client.conn.Close()
}

func (client *aircatDevice) cleanup() {
	if client.conn != nil {
		client.conn.Close()
	}
}

//Config for running programme
type Config struct {
	ServerAddr     string //default as ":9000"
	RESTServerAddr string //default as ":8080"
	InfluxdbServer string //default as "influxdb:8086"
	RedisServer    string //default as "redis:6379"
	ClusterMode    bool   //default as false
	InitDelay      int    //deday initial run after N seconds
}

var configs Config

//LoadConfig load config file
func LoadConfig(file string) error {
	configFile, err := os.Open(file)
	if err != nil {
		return err
	}
	defer configFile.Close()
	jsonParse := json.NewDecoder(configFile)
	if err = jsonParse.Decode(&configs); err != nil {
		return err
	}

	return nil
}

//ParseConfig parse args from cmdline
func ParseConfig() error {
	var help bool
	flag.BoolVar(&help, "h", false, "show help")
	flag.StringVar(&configs.InfluxdbServer, "influxdb-addr", "influxdb:8086", "set influxdb server address")
	flag.StringVar(&configs.RedisServer, "redis-addr", "redis:6379", "set redis server address")
	flag.BoolVar(&configs.ClusterMode, "cluster", false, "run aircat-srv as cluster mode")
	flag.IntVar(&configs.InitDelay, "init-delay", 0, "initial delay seconds(1-59), after redis/influxdb ready")
	flag.Parse()
	if help || len(flag.Args()) > 0 {
		fmt.Println("usage: aircat-srv --influxdb-addr INFLUXDB_ADDRESS [--cluster --redis-addr REDIS_ADDRESS]")
		flag.PrintDefaults()
		os.Exit(1)
	}
	configs.Default()
	return nil
}

//Default load default confi
func (c *Config) Default() {
	if c.ServerAddr == "" {
		c.ServerAddr = ":9000"
	}
	if c.RESTServerAddr == "" {
		c.RESTServerAddr = ":8080"
	}
	if c.InfluxdbServer == "" {
		c.InfluxdbServer = "influxdb:8086"
	}
	if c.RedisServer == "" {
		c.RedisServer = "redis:6379"
	}
}
