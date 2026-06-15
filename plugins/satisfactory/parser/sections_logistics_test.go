package main

import (
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

func collectLogisticsObj(s *saveState, classPath, instance string, props map[string]any) {
	o := sav.Object{ObjectHeader: sav.ObjectHeader{ClassPath: classPath, InstanceName: instance}}
	kind := logisticsKind(o.ObjectHeader)
	s.collectLogistics(kind, o, &sav.ObjectData{Properties: props})
}

func logisticsState() *saveState {
	s := newSaveState(testHeader())
	containerCls := "/Game/FactoryGame/Buildable/Factory/StorageContainerMk2/" +
		"Build_StorageContainerMk2.Build_StorageContainerMk2_C"
	collectLogisticsObj(s, containerCls, "L:P.Build_StorageContainerMk2_C_1", nil)
	// Two storage inventories holding iron plates.
	stacks := map[string]any{"mInventoryStacks": []any{
		map[string]any{
			"Item":     sav.InventoryItem{ItemClass: "/Game/X/Desc_IronPlate.Desc_IronPlate_C"},
			"NumItems": int64(400),
		},
	}}
	collectLogisticsObj(s, "/Script/FactoryGame.FGInventoryComponent",
		"L:P.Build_StorageContainerMk2_C_1.StorageInventory", stacks)
	collectLogisticsObj(s, "/Script/FactoryGame.FGInventoryComponent",
		"L:P.Build_StorageContainerMk2_C_2.StorageInventory", stacks)
	// Central storage with one depot item.
	collectLogisticsObj(s, "/Script/FactoryGame.FGCentralStorageSubsystem", "L:P.CentralStorage", map[string]any{
		"mStoredItems": []any{
			map[string]any{
				"ItemClass": sav.ObjectRef{Path: "/Game/X/Desc_Cement.Desc_Cement_C"},
				"Amount":    int64(2500),
			},
		},
	})
	// Train network.
	collectLogisticsObj(s, "/Game/FactoryGame/Buildable/Vehicle/Train/-Shared/BP_Train.BP_Train_C", "L:P.T1", nil)
	locomotiveCls := "/Game/FactoryGame/Buildable/Vehicle/Train/Locomotive/BP_Locomotive.BP_Locomotive_C"
	collectLogisticsObj(s, locomotiveCls, "L:P.L1", nil)
	stationCls := "/Game/FactoryGame/Buildable/Factory/Train/Station/Build_TrainStation.Build_TrainStation_C"
	collectLogisticsObj(s, stationCls, "L:P.S1", nil)
	collectLogisticsObj(s, "/Script/FactoryGame.FGTrainStationIdentifier", "L:P.I1",
		map[string]any{"mStationName": "Almet Copper Mine"})
	collectLogisticsObj(s, "/Script/FactoryGame.FGRailroadTimeTable", "L:P.TT1",
		map[string]any{"mStops": []any{map[string]any{}, map[string]any{}}})
	// Drones + vehicles.
	droneCls := "/Game/FactoryGame/Buildable/Factory/DroneStation/BP_DroneTransport.BP_DroneTransport_C"
	collectLogisticsObj(s, droneCls, "L:P.D1", nil)
	collectLogisticsObj(s, "/Script/FactoryGame.FGDroneStationInfo", "L:P.DS1",
		map[string]any{"mBuildingTag": "Fuel Port"})
	collectLogisticsObj(s, "/Game/FactoryGame/Buildable/Vehicle/Truck/BP_Truck.BP_Truck_C", "L:P.V1", nil)
	return s
}

func TestBuildStorageSection(t *testing.T) {
	data := logisticsState().buildStorageSection()

	containers, _ := data["containers"].(map[string]int)
	if containers["Storage Container Mk2"] != 1 {
		t.Errorf("containers = %v", containers)
	}
	items, _ := data["itemsInStorage"].([]map[string]any)
	if len(items) != 1 || items[0]["name"] != "Iron Plate" || items[0]["count"] != int64(800) {
		t.Errorf("itemsInStorage = %v, want Iron Plate x800 (two containers summed)", items)
	}
	depot, _ := data["dimensionalDepot"].(map[string]any)
	depotItems, _ := depot["items"].([]map[string]any)
	if len(depotItems) != 1 || depotItems[0]["name"] != "Concrete" || depotItems[0]["count"] != int64(2500) {
		t.Errorf("dimensionalDepot = %v", depot)
	}
}

func TestBuildLogisticsSection(t *testing.T) {
	data := logisticsState().buildLogisticsSection()

	trains, _ := data["trains"].(map[string]any)
	if trains["trains"] != 1 || trains["locomotives"] != 1 || trains["stations"] != 1 {
		t.Errorf("trains = %v", trains)
	}
	names, _ := trains["stationNames"].([]string)
	if len(names) != 1 || names[0] != "Almet Copper Mine" {
		t.Errorf("stationNames = %v", names)
	}
	if trains["timetables"] != 1 || trains["timetableStops"] != 2 {
		t.Errorf("timetables = %v stops = %v", trains["timetables"], trains["timetableStops"])
	}
	drones, _ := data["drones"].(map[string]any)
	droneNames, _ := drones["stationNames"].([]string)
	if drones["drones"] != 1 || len(droneNames) != 1 || droneNames[0] != "Fuel Port" {
		t.Errorf("drones = %v", drones)
	}
	vehicles, _ := data["vehicles"].(map[string]any)
	byType, _ := vehicles["byType"].(map[string]int)
	if byType["Truck"] != 1 {
		t.Errorf("vehicles = %v", vehicles)
	}
}

func TestBuildResourceNodesSection(t *testing.T) {
	s := newSaveState(testHeader())
	miner := "/Game/FactoryGame/Buildable/Factory/MinerMk3/Build_MinerMk3.Build_MinerMk3_C"
	// Two miners on distinct nodes, one sharing the first node's path.
	collectMachine(s, miner, map[string]any{
		"mExtractableResource": sav.ObjectRef{Path: "P:PersistentLevel.BP_ResourceNode1"},
	})
	collectMachine(s, miner, map[string]any{
		"mExtractableResource": sav.ObjectRef{Path: "P:PersistentLevel.BP_ResourceNode2"},
	})
	collectMachine(s, miner, map[string]any{
		"mExtractableResource": sav.ObjectRef{Path: "P:PersistentLevel.BP_ResourceNode1"},
	})

	data := s.buildResourceNodesSection()
	if data["occupiedNodes"] != 2 {
		t.Errorf("occupiedNodes = %v, want 2 (distinct nodes)", data["occupiedNodes"])
	}
	groups, _ := data["byExtractor"].([]map[string]any)
	if len(groups) != 1 || groups[0]["count"] != 3 {
		t.Errorf("byExtractor = %v", groups)
	}
}

func TestBuildLogisticsEmptyNote(t *testing.T) {
	data := newSaveState(testHeader()).buildLogisticsSection()
	if data["note"] == nil {
		t.Errorf("empty logistics should carry a note, got %v", data)
	}
}
