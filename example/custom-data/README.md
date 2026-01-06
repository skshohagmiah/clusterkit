# Custom Data Example

This example demonstrates ClusterKit's custom data storage feature, which allows you to store application-specific metadata that gets replicated across all cluster nodes via Raft consensus.

## What It Demonstrates

- **Feature Flags** - Store global feature toggles accessible from any node
- **Application Config** - Shared settings replicated across the cluster
- **Global Counters** - Cluster-wide state that all nodes can access
- **Raft Consensus** - All custom data is replicated via Raft for consistency

## Running the Example

```bash
cd example/custom-data
./run.sh
```

This will:
1. Start a 3-node cluster (node-1, node-2, node-3)
2. Bootstrap node sets custom data (feature flags, config, counter)
3. All nodes read and display the replicated custom data
4. Show logs demonstrating data consistency across nodes

## Use Cases

### Feature Flags
```go
featureFlags := map[string]bool{
    "new_ui": true,
    "beta_feature": false,
}
flagsJSON, _ := json.Marshal(featureFlags)
ck.SetCustomData("feature_flags", flagsJSON)

// Read on any node
flagsData, _ := ck.GetCustomData("feature_flags")
var flags map[string]bool
json.Unmarshal(flagsData, &flags)

if flags["new_ui"] {
    // Enable new UI
}
```

### Application Configuration
```go
appConfig := map[string]interface{}{
    "max_connections": 100,
    "timeout_seconds": 30,
}
configJSON, _ := json.Marshal(appConfig)
ck.SetCustomData("app_config", configJSON)
```

### Global Counters
```go
counter := map[string]int{"request_count": 0}
counterJSON, _ := json.Marshal(counter)
ck.SetCustomData("global_counter", counterJSON)
```

## API Methods

- `SetCustomData(key, value)` - Store data (leader only, goes through Raft)
- `GetCustomData(key)` - Retrieve data (local read, any node)
- `DeleteCustomData(key)` - Remove data (leader only, goes through Raft)
- `ListCustomDataKeys()` - List all keys (local read, any node)

## Limitations

- **Size limit**: 1MB per value (designed for metadata, not large datasets)
- **Write latency**: Writes go through Raft consensus (slower than local writes)
- **Leader-only writes**: Only the leader can propose changes
- **Not for large datasets**: Use partition-based storage for that

## Output Example

```
✅ Node node-1 started on :8080
🔧 Bootstrap node: Setting custom data...
✅ Set feature flags: map[beta_feature:false dark_mode:true new_ui:true]
✅ Set app config: map[api_version:v2 max_connections:100 timeout_seconds:30]
✅ Set global counter: map[request_count:0]

📖 Reading custom data from cluster...
📋 Available custom data keys: [feature_flags app_config global_counter]
🚩 Feature flags: map[beta_feature:false dark_mode:true new_ui:true]
   ➡️  New UI is ENABLED
   ➡️  Dark mode is ENABLED
⚙️  App config: map[api_version:v2 max_connections:100 timeout_seconds:30]
🔢 Global counter: map[request_count:0]

✅ Node node-1 successfully read custom data from cluster!
   All nodes have access to the same custom data via Raft consensus
```

## Clean Up

```bash
rm -rf ./data
```
