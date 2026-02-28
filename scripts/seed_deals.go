package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	log.Println("Seeding Deals...")

	// Deal 1: NORMAL Priority (< 1,000,000)
	d1 := loadCleanDeal()
	d1["dealId"] = "DL-SEED-001"
	d1["dealName"] = "Acme Corp Expansion - Normal"
	d1["status"] = "DOCUMENT_VERIFICATION"

	umbrella := d1["umbrellaLimit"].(map[string]interface{})
	approved := umbrella["approvedLimit"].(map[string]interface{})
	approved["amount"] = 500000.0 // Normal Priority

	borrowingEntities := d1["borrowingEntities"].([]interface{})
	be1 := borrowingEntities[0].(map[string]interface{})
	facilities := be1["facilities"].([]interface{})
	fac1 := facilities[0].(map[string]interface{})

	fac1Limit := fac1["facilityLimit"].(map[string]interface{})
	fac1Limit["amount"] = 500000.0
	fac1Consumption := fac1["umbrellaConsumption"].(map[string]interface{})
	fac1ConsAmount := fac1Consumption["consumptionAmount"].(map[string]interface{})
	fac1ConsAmount["amount"] = 500000.0

	be1["facilities"] = []interface{}{fac1}
	d1["borrowingEntities"] = []interface{}{be1}

	sendDeal("DL-SEED-001", d1)

	// Deal 2: URGENT Priority (> 5,000,000)
	d2 := loadCleanDeal()
	d2["dealId"] = "DL-SEED-002"
	d2["dealName"] = "Global Tech Scaling - Urgent"
	d2["status"] = "DOCUMENT_VERIFICATION"

	u2 := d2["umbrellaLimit"].(map[string]interface{})
	a2 := u2["approvedLimit"].(map[string]interface{})
	a2["amount"] = 8000000.0 // Urgent Priority

	be2 := d2["borrowingEntities"].([]interface{})[0].(map[string]interface{})
	fac2 := be2["facilities"].([]interface{})[0].(map[string]interface{})

	fac2Limit := fac2["facilityLimit"].(map[string]interface{})
	fac2Limit["amount"] = 8000000.0
	fac2Consumption := fac2["umbrellaConsumption"].(map[string]interface{})
	fac2ConsAmount := fac2Consumption["consumptionAmount"].(map[string]interface{})
	fac2ConsAmount["amount"] = 8000000.0

	be2["facilities"] = []interface{}{fac2}
	d2["borrowingEntities"] = []interface{}{be2}

	sendDeal("DL-SEED-002", d2)
}

func loadCleanDeal() map[string]interface{} {
	data, err := os.ReadFile("../IngestDealRequirements/SampleJSON.json")
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Fatalf("failed to parse json: %v", err)
	}
	return payload
}

func sendDeal(dealId string, snapshot map[string]interface{}) {
	reqBody := map[string]interface{}{
		"snapshot":          snapshot,
		"snapshotTimestamp": time.Now().UTC().Format(time.RFC3339),
		"sourceSystem":      "ORIGINATION",
		"correlationId":     "CORR-" + dealId,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Fatalf("failed to marshal: %v", err)
	}

	req, err := http.NewRequest("POST", "http://localhost:8080/ingest/deals", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tenant-id", "DEFAULT")
	req.Header.Set("x-idempotency-key", "IDEMP-"+dealId+"-"+fmt.Sprint(time.Now().Unix()))

	log.Printf("Sending Deal %s...", dealId)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("Response Status: %s", resp.Status)
	log.Printf("Response Body: %s", string(body))
}
