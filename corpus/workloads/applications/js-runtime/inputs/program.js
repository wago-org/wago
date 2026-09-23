var state = 0x9e3779b9;

function nextRandom() {
  state ^= state << 13;
  state ^= state >>> 17;
  state ^= state << 5;
  return state >>> 0;
}

var levels = ["DEBUG", "INFO", "WARN", "ERROR"];
var buckets = [];
for (var bucketIndex = 0; bucketIndex < 97; bucketIndex++) {
  buckets.push({ count: 0, latency: 0, errors: 0, bytes: 0 });
}

var eventPattern = /^(\d+)\|svc-(\d+)\|([A-Z]+)\|(\d+)\|([a-z0-9]+)$/;
var accepted = 0;
var rejected = 0;
var rolling = 0;

for (var eventIndex = 0; eventIndex < 50000; eventIndex++) {
  var random = nextRandom();
  var service = random % 97;
  var level = levels[(random >>> 8) & 3];
  var latency = ((random >>> 10) % 4000) + 1;
  var payload = ((random ^ (eventIndex * 2654435761)) >>> 0).toString(36);
  var line = eventIndex + "|svc-" + service + "|" + level + "|" + latency + "|" + payload;
  var match = eventPattern.exec(line);
  if (match === null) {
    rejected++;
    continue;
  }

  var parsedService = +match[2];
  var parsedLatency = +match[4];
  var bucket = buckets[parsedService];
  bucket.count++;
  bucket.latency += parsedLatency;
  bucket.bytes += match[5].length;
  if (match[3] === "ERROR") bucket.errors++;
  accepted++;
  rolling = (rolling + ((parsedLatency * 33) ^ random ^ eventIndex)) >>> 0;
}

var ranked = buckets.map(function (bucket, service) {
  return {
    service: service,
    score: bucket.latency + bucket.errors * 10000 + bucket.bytes * 17,
    count: bucket.count,
  };
});
ranked.sort(function (left, right) {
  return right.score - left.score || left.service - right.service;
});

var totalLatency = buckets.reduce(function (sum, bucket) { return sum + bucket.latency; }, 0);
var totalErrors = buckets.reduce(function (sum, bucket) { return sum + bucket.errors; }, 0);
var top = ranked.slice(0, 8).map(function (entry) {
  return entry.service + ":" + entry.score + ":" + entry.count;
}).join(",");

console.log(accepted, rejected, totalLatency, totalErrors, rolling, top);
