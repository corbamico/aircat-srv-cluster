package aircat

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/gomodule/redigo/redis"
)

const (
	maxCacheChannelCap     = 10000 //see ref struct#connCache.channel
	maxCacheChannelTimeout = 500   //* time.Millisecond
)

//connCache has key-value
type connCache struct {
	channel chan itemMessage
}

const (
	cmdInsert uint32 = iota
	cmdDelete
	cmdGet
	cmdDestory
)

type itemMessage struct {
	cmd    uint32
	key    string
	value  interface{}
	result chan interface{}
}

type itemCache struct {
	conn net.Conn //Conn to device, used for write to tcp
	msg  message  //first message from device
}

func newConnCache() connCache {
	channel := make(chan itemMessage, maxCacheChannelCap)
	cache := make(map[string]interface{})

	go func() {
		for cmd := range channel {
			switch cmd.cmd {
			case cmdInsert:
				cache[cmd.key] = cmd.value
			case cmdDelete:
				delete(cache, cmd.key)
			case cmdGet:
				value, _ := cache[cmd.key]
				glog.V(logDebug).Infof("cmdGet,get value=%v", value)
				cmd.result <- itemMessage{cmdGet, cmd.key, value, nil}
			case cmdDestory:
				close(channel)
				return
			}
		}
	}()
	return connCache{
		channel: channel,
	}
}

func (s *connCache) insert(key string, value interface{}) {
	s.channel <- itemMessage{cmdInsert, key, value, nil}
}

func (s *connCache) delete(key string) {
	s.channel <- itemMessage{cmdDelete, key, nil, nil}
}

func (s *connCache) get(key string) (interface{}, error) {
	result := make(chan interface{})
	s.channel <- itemMessage{cmdGet, key, nil, result}
	select {
	case r, ok := <-result:
		if ok {
			return r.(itemMessage).value, nil
		}
		return nil, errors.New("channel error")
	case <-time.After(maxCacheChannelTimeout * time.Millisecond):
		return nil, errors.New("timeout")
	}
}
func (s *connCache) destory() {
	s.channel <- itemMessage{cmdDestory, "", nil, nil}
}

/// redis store {mac->restsrv_address}
///
type connClusterCache struct {
	redisClient redis.Conn
	httpClient  http.Client
}

func newConnClusterCache() connClusterCache {
	redisClient, err := redis.Dial("tcp", configs.RedisServer, redis.DialConnectTimeout(5*time.Second))
	if err != nil {
		glog.Warningf("redis dial error=%s", err.Error())
	}
	return connClusterCache{
		redisClient,
		http.Client{Timeout: 1 * time.Second},
	}
}
func (s *connClusterCache) getL2Cached(key string) (string, error) {
	//mac->{server_ip}
	if s.redisClient != nil {
		res, err := redis.String(s.redisClient.Do("GET", key))
		return res, err
	}
	return "", errors.New("no redis connection")
}
func (s *connClusterCache) insertL2Cached(key string, value string) error {
	if s.redisClient != nil {
		_, err := s.redisClient.Do("SET", key, value)
		glog.V(logWarning).Infof("insert redis (key=%s),(value=%s), with result=%v\n", key, value, err)
		return err
	}
	return errors.New("no redis connection")
}
func (s *connClusterCache) redirectJSON(mac string, json string) error {
	var err error
	var addr string
	var url string
	if addr, err = s.getL2Cached(mac); err != nil {
		glog.V(logWarning).Infof("query redis (mac=%s), error=%s\n", mac, err.Error())
		return err
	}
	glog.V(logDebug).Infof("query redis (mac=%s) success, result=%s\n", mac, addr)

	url = fmt.Sprintf("http://%s:8080", addr)
	if _, err = s.httpClient.Post(url, "", strings.NewReader(json)); err != nil {
		return err
	}
	return nil
}
