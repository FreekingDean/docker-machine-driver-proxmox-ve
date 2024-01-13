package driver

import (
	"time"

	"github.com/labstack/gommon/log"
)

func (d *Driver) debugf(format string, v ...interface{}) {
	if d.driverDebug {
		log.Infof(format, v...)
	}
}

func (d *Driver) debug(v ...interface{}) {
	if d.driverDebug {
		log.Info(v...)
	}
}

type retryfunc func() error

func (dr *Driver) retry(f retryfunc, d time.Duration, c int) error {
	var err error
	for i := 0; i < c; i++ {
		err = f()
		if err == nil {
			return nil
		}
		dr.debugf("error attempting %w", err)
		time.Sleep(d)
	}
	return err
}
