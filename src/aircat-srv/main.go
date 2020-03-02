package main

import (
	"aircat-srv/aircat"

	"github.com/golang/glog"
)

func main() {
	if err := aircat.ParseConfig(); err != nil {
		glog.Fatalln(err)
	}
	s := aircat.NewAircatServer()
	s.Run()
	return
}
