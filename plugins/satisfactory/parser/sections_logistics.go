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
		s.containerPos[o.InstanceName] = o.Translation
	case "centralStorage":
		s.centralStorage = od
	case "storageInventory":
		stacks := inventoryStacks(od)
		for _, st := range stacks {
			s.storedItems[st.itemClass] += st.count
		}
		s.storageInventories = append(s.storageInventories, storageInv{
			owner: strings.TrimSuffix(o.InstanceName, ".StorageInventory"),
			items: stacks,
		})
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

	if byBase := s.storageByBase(); len(byBase) > 0 {
		data["byBase"] = byBase
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

// storageByBase buckets each positioned storage container's contents into its
// geography base. Containers whose owning actor had no captured position (e.g.
// the dimensional depot uploader, which is not a StorageContainerMk) are
// omitted here but remain in the global itemsInStorage total. Bases are ordered
// by id (matching the geography section) and items within a base by count desc.
// storageBucketsByBaseID groups positioned storage contents into per-base item
// stocks (base id -> item class -> count). Shared by the storage section's
// byBase display and the flow_balance buffer column, so both read one number.
func (s *saveState) storageBucketsByBaseID() map[int]map[string]int64 {
	if s.storageBuckets != nil {
		return s.storageBuckets
	}
	idx := s.bases()
	buckets := map[int]map[string]int64{}
	for _, inv := range s.storageInventories {
		pos, ok := s.containerPos[inv.owner]
		if !ok {
			continue
		}
		base := idx.assign(pos)
		if base < 0 {
			continue
		}
		b := buckets[base]
		if b == nil {
			b = map[string]int64{}
			buckets[base] = b
		}
		for _, st := range inv.items {
			b[st.itemClass] += st.count
		}
	}
	s.storageBuckets = buckets
	return buckets
}

func (s *saveState) storageByBase() []map[string]any {
	idx := s.bases()
	buckets := s.storageBucketsByBaseID()

	baseIDs := make([]int, 0, len(buckets))
	for id := range buckets {
		baseIDs = append(baseIDs, id)
	}
	sort.Ints(baseIDs)

	out := make([]map[string]any, 0, len(baseIDs))
	for _, id := range baseIDs {
		type entry struct {
			cls   string
			count int64
		}
		list := make([]entry, 0, len(buckets[id]))
		for cls, count := range buckets[id] {
			list = append(list, entry{cls, count})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].count != list[j].count {
				return list[i].count > list[j].count
			}
			return list[i].cls < list[j].cls
		})
		items := make([]map[string]any, 0, len(list))
		for _, e := range list {
			items = append(items, map[string]any{
				"name": displayName(e.cls), "classPath": e.cls, "count": e.count,
			})
		}
		out = append(out, map[string]any{
			"base":  baseName(idx.bases[id], s.mapMarkers),
			"items": items,
		})
	}
	return out
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
