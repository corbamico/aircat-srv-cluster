package aircat

import (
	"io/ioutil"
	"net/http"
	"regexp"

	"github.com/golang/glog"
)

/*
APIs:
	1.query current measure, ignore mac
	GET v1/aircat/{mac}
	Response: {"temperature"=1,"humidity"=2,"value"=3,"hcho"=4}
	2.change brightness. currently ignore mac, we have only one aircat.
	PUT v1/aricat/{mac}
		{"brightness":"100","type":2}
*/

//restServer run at port 9000
type restServer struct {
	controlChan  chan controlMessage
	aircatServer *Server
}

//newrestServer create a restServer
func newrestServer(controlChan chan controlMessage) *restServer {
	return &restServer{controlChan: controlChan}
}

func (s *restServer) setAircatServer(aircatServer *Server) {
	s.aircatServer = aircatServer
}

//Run restServer at port 8080
func (s *restServer) Run() {
	go func() {
		http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
		http.HandleFunc("/v1/aircat/", handlerFunc(s))
		glog.Infof("REST Server run at %s\n", configs.RESTServerAddr)
		glog.Fatalln(http.ListenAndServe(configs.RESTServerAddr, nil))
	}()
}

func handlerFunc(s *restServer) func(w http.ResponseWriter, r *http.Request) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		mac := r.URL.Path[len("/v1/aircat/"):]

		if !validateMAC(mac) {
			glog.V(logDebug).Infof("get invalid mac=%s from url", mac)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.Method == http.MethodGet {
			w.Header().Add("Content-Type", "application/json")

			body, err := s.aircatServer.w.queryLast(mac)
			if err == nil {
				w.Write(body)
				return
			}
			glog.V(logDebug).Infof("Query mac(%s), result is error(%s)\n", mac, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPut {
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
			}
			glog.V(logDebug).Infof("send json to aircatsrv routine from rest-srv, mac=%s,body=%s\n", mac, string(body))
			s.controlChan <- controlMessage{mac, string(body)}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	return handler
}
func validateMAC(mac string) bool {
	matched, _ := regexp.MatchString(`^[0-9a-f]{12}$`, mac)
	return matched
}
