local buckets = {}
for key = 0, 256 do
  buckets[key] = { count = 0, total = 0, checksum = 0 }
end

local state = 0x243f6a88
for i = 1, 1000000 do
  state = (state ~ (state << 13)) & 0xffffffff
  state = (state ~ (state >> 17)) & 0xffffffff
  state = (state ~ (state << 5)) & 0xffffffff
  local key = state % 257
  local value = ((state >> 8) ~ (i * 2654435761)) & 0x7fffffff
  local bucket = buckets[key]
  bucket.count = bucket.count + 1
  bucket.total = (bucket.total + value) & 0xffffffffffff
  bucket.checksum = (bucket.checksum ~ value ~ i) & 0xffffffff
end

local ranked = {}
local total = 0
local checksum = 0
for key = 0, 256 do
  local bucket = buckets[key]
  total = total + bucket.total
  checksum = (checksum ~ bucket.checksum) & 0xffffffff
  ranked[#ranked + 1] = {
    key = key,
    score = bucket.total + bucket.count * 1000003,
    count = bucket.count,
  }
end

table.sort(ranked, function(left, right)
  if left.score == right.score then return left.key < right.key end
  return left.score > right.score
end)

local top = {}
for index = 1, 8 do
  local entry = ranked[index]
  top[#top + 1] = string.format("%d:%d:%d", entry.key, entry.score, entry.count)
end

print(string.format("1000000,%d,%u,%s", total, checksum, table.concat(top, ",")))
