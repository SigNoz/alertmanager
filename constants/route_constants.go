package constants

import ( 
	"time"
)

func RouteOptsGroupWait() time.Duration {
	groupWaitInt := GetOrDefaultEnvInt("ALERTMANAGER_GROUP_WAIT", 30)
	return time.Duration(groupWaitInt) * time.Second
}

func RouteOptsGroupInterval() time.Duration {
	groupIntervalInt := GetOrDefaultEnvInt("ALERTMANAGER_GROUP_INTERVAL", 300)
	return time.Duration(groupIntervalInt) * time.Second
}

func RouteOptsRepeatInterval() time.Duration {
	repeatInt := GetOrDefaultEnvInt("ALERTMANAGER_REPEAT_INTERVAL", 240)
	return time.Duration(repeatInt) * time.Minute
}
