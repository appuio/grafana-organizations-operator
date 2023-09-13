package controller

import (
	"errors"
	"fmt"
	grafana "github.com/grafana/grafana-api-golang-client"
)

type Dashboard struct {
	Folder string
	Data   map[string]interface{}
}

func configureDashboard(dashboardModel map[string]interface{}, dataSource *grafana.DataSource) error {
	delete(dashboardModel, "id")
	delete(dashboardModel, "uid")
	delete(dashboardModel, "version")
	delete(dashboardModel, "time")

	var dataSourceMap = make(map[string]string)
	dataSourceMap["type"] = dataSource.Type
	dataSourceMap["uid"] = dataSource.UID
	if panels, ok := dashboardModel["panels"]; ok {
		panelsArray, ok := panels.([]interface{})
		if !ok {
			return errors.New("Invalid dashboard format: 'panels' does not contain array")
		}
		for i, panel := range panelsArray {
			panelMap, ok := panel.(map[string]interface{})
			if !ok {
				return errors.New(fmt.Sprintf("Invalid dashboard format: 'panels[%d]' is not a map", i))
			}
			panelMap["datasource"] = dataSourceMap
		}
	}

	if templating, ok := dashboardModel["templating"]; ok {
		templatingMap, ok := templating.(map[string]interface{})
		if !ok {
			return errors.New("Invalid dashboard format: 'templating' does not contain map")
		}
		if templatingList, ok := templatingMap["list"]; ok {
			templatingListArray, ok := templatingList.([]interface{})
			if !ok {
				return errors.New("Invalid dashboard format: 'templating.list' does not contain array")
			}
			for i, template := range templatingListArray {
				templateMap, ok := template.(map[string]interface{})
				if !ok {
					return errors.New(fmt.Sprintf("Invalid dashboard format: 'templating.list[%d]' is not a map", i))
				}
				templateMap["datasource"] = dataSourceMap
				delete(templateMap, "current")
			}
		}
	}

	return nil
}
