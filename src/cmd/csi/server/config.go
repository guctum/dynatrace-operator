package server

import (
	"github.com/Dynatrace/dynatrace-operator/src/logger"
)

var (
	_ = logger.Factory.GetLogger("csi-launcher")
)
