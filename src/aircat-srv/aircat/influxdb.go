package aircat

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/golang/glog"
)

type writer interface {
	write(mac string, json string)
}

//example for line procotol:
//curl -i -XPOST 'http://localhost:8086/write?db=mydb' --data-binary 'cpu_load_short,host=server01,region=us-west value=0.64 1434055562000000000'
//we use as :
//aircat,mac=xxx temperature=1,humidity=2,value=3,hcho=4
type influxdb struct {
	addr   string
	client http.Client
}

func newInfluxdb(addr string) influxdb {
	return influxdb{addr, http.Client{Timeout: 2 * time.Second}}
}

func (s *influxdb) write(mac string, json string) {
	if s.addr == "" {
		println(mac, json)
		return
	}
	if line := formatLineProtocol(mac, json); line != "" {
		//we ignore error
		go func() {
			url := fmt.Sprintf("http://%s/write?db=aircat", s.addr)
			if _, err := s.client.Post(url, "", strings.NewReader(line)); err != nil {
				glog.Errorf("Error sending request to influxdb, url=%s,error=%s", url, err.Error())
			}
		}()
	}

}

func (s *influxdb) queryLast(mac string) ([]byte, error) {

	url := fmt.Sprintf("http://%s/query?db=aircat", s.addr)
	sql := `q=select last(*) from aircat where mac='%s'`
	sql = fmt.Sprintf(sql, mac)

	r, err := s.client.Post(url, "application/x-www-form-urlencoded", strings.NewReader(sql))
	if err != nil {
		glog.Errorf("Error sending request to influxdb, url=%s,error=%s", url, err.Error())
		return nil, err
	}
	return ioutil.ReadAll(r.Body)
}

func formatLineProtocol(mac string, js string) string {
	var air AirMeasure
	if err := json.Unmarshal([]byte(js), &air); err != nil {
		return ""
	}
	return fmt.Sprintf("aircat,mac=%s humidity=%s,temperature=%s,value=%s,hcho=%s", mac, air.Humidity, air.Temperature, air.Value, air.Hcho)
}

//AirMeasure reported from device
type AirMeasure struct {
	Humidity    string
	Temperature string
	Value       string
	Hcho        string
}
