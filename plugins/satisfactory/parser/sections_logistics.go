package main

import (
	"sort"
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// logisticsKind classifies storage/transport classes (probed on a 650-hour
// megafactory). High-volume passive rail pieces (tracks, signals — 6k+
// objects) are deliberately not extracted; counts of those add nothing a
// planner needs.
func logisticsKind(o sav.ObjectHeader) string {
	cls := o.ClassPath
	switch {
	case strings.Contains(cls, "/Buildable/Factory/StorageContainerMk"):
		return "container"
	case strings.HasSuffix(cls, ".FGCentralStorageSubsystem"):
		return "centralStorage"
	case strings.HasSuffix(cls, ".BP_Train_C"):
		return "train"
	case strings.HasSuffix(cls, ".BP_Locomotive_C"):
		return "locomotive"
	case strings.HasSuffix(cls, ".BP_FreightWagon_C"):
		return "wagon"
	case strings.HasSuffix(cls, ".Build_TrainStation_C"):
		return "trainStation"
	case strings.HasSuffix(cls, ".FGTrainStationIdentifier"):
		return "stationIdentifier"
	case strings.HasSuffix(cls, ".FGRailroadTimeTable"):
		return "timetable"
	case strings.HasSuffix(cls, ".BP_DroneTransport_C"):
		return "drone"
	case strings.HasSuffix(cls, ".FGDroneStationInfo"):
		return "droneStationInfo"
	case strings.HasSuffix(cls, ".Build_TruckStation_C"):
		return "truckStation"
	case strings.Contains(cls, "/Buildable/Vehicle/Truck/"),
		strings.Contains(cls, "/Buildable/Vehicle/Tractor/"),
		strings.Contains(cls, "/Buildable/Vehicle/Explorer/"),
		strings.Contains(cls, "/Buildable/Vehicle/Golfcart/"):
		return "vehicle"
	}
	// Storage container + dimensional depot uploader contents.
	if strings.Contains(cls, "FGInventoryComponent") &&
		strings.HasSuffix(o.InstanceName, ".StorageInventory") {
		return "storageInventory"
	}
	return ""
}

func (s *saveState) collectLogistics(kind string, o sav.Object, od *sav.ObjectData) {
	switch kind {
	case "container":
		s.containerCounts[displayName(o.ClassPath)]++
	case "centralStorage":
		s.centralStorage = od
	case "storageInventory":
		for _, item := range inventoryItems(od) {
			cls, clsOK := item["classPath"].(string)
			count, countOK := item["count"].(int64)
			if clsOK && countOK {
				s.storedItems[cls] += count
			}
		}
	case "train":
		s.trains++
	case "locomotive":
		s.locomotives++
	case "wagon":
		s.wagons++
	case "trainStation":
		s.trainStations++
	case "stationIdentifier":
		if name, ok := prop[string](od, "mStationName"); ok && name != "" {
			s.trainStationNames = append(s.trainStationNames, name)
		}
	case "timetable":
		s.timetables++
		if stops, ok := prop[[]any](od, "mStops"); ok {
			s.timetableStops += len(stops)
		}
	case "drone":
		s.drones++
	case "droneStationInfo":
		if tag, ok := prop[string](od, "mBuildingTag"); ok && tag != "" {
			s.droneStationNames = append(s.droneStationNames, tag)
		}
	case "truckStation":
		s.truckStations++
	case "vehicle":
		s.vehicleCounts[displayName(o.ClassPath)]++
	}
}

func (s *saveState) buildStorageSection() map[string]any {
	type itemTotal struct {
		cls   string
		count int64
	}
	totals := make([]itemTotal, 0, len(s.storedItems))
	for cls, count := range s.storedItems {
		totals = append(totals, itemTotal{cls, count})
	}
	sort.Slice(totals, func(i, j int) bool { return totals[i].count > totals[j].count })
	items := make([]map[string]any, 0, len(totals))
	for _, t := range totals {
		items = append(items, map[string]any{
			"name": displayName(t.cls), "classPath": t.cls, "count": t.count,
		})
	}

	data := map[string]any{
		"containers":     s.containerCounts,
		"itemsInStorage": items,
	}

	if stored, ok := prop[[]any](s.centralStorage, "mStoredItems"); ok {
		depot := make([]map[string]any, 0, len(stored))
		for _, raw := range stored {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ref, ok := entry["ItemClass"].(sav.ObjectRef)
			if !ok {
				continue
			}
			amount, ok := entry["Amount"].(int64)
			if !ok {
				amount = 0
			}
			depot = append(depot, map[string]any{
				"name": displayName(ref.Path), "classPath": ref.Path, "count": amount,
			})
		}
		sort.Slice(depot, func(i, j int) bool {
			a, aOK := depot[i]["count"].(int64)
			b, bOK := depot[j]["count"].(int64)
			return aOK && bOK && a > b
		})
		data["dimensionalDepot"] = map[string]any{"items": depot}
	}
	return data
}

func (s *saveState) buildLogisticsSection() map[string]any {
	sort.Strings(s.trainStationNames)
	sort.Strings(s.droneStationNames)

	data := map[string]any{}
	if s.trains > 0 || s.trainStations > 0 {
		data["trains"] = map[string]any{
			"trains":         s.trains,
			"locomotives":    s.locomotives,
			"freightWagons":  s.wagons,
			"stations":       s.trainStations,
			"stationNames":   s.trainStationNames,
			"timetables":     s.timetables,
			"timetableStops": s.timetableStops,
		}
	}
	if s.drones > 0 || len(s.droneStationNames) > 0 {
		data["drones"] = map[string]any{
			"drones":       s.drones,
			"stationNames": s.droneStationNames,
		}
	}
	if len(s.vehicleCounts) > 0 || s.truckStations > 0 {
		data["vehicles"] = map[string]any{
			"byType":        s.vehicleCounts,
			"truckStations": s.truckStations,
		}
	}
	if len(data) == 0 {
		data["note"] = "no trains, drones, or vehicles built yet"
	}
	return data
}

func (s *saveState) buildResourceNodesSection() map[string]any {
	nodes := map[string]bool{}
	for _, r := range s.extractors {
		if r.node != "" {
			nodes[r.node] = true
		}
	}
	byExtractor := groupMachines(
		s.extractors,
		"extractor",
		func(r machineRecord) string { return r.building },
	)
	return map[string]any{
		"occupiedNodes": len(nodes),
		"byExtractor":   describeGroups(byExtractor),
	}
}
