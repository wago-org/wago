local counts = {}
for i = 1, 5000 do
  local key = i % 17
  counts[key] = (counts[key] or 0) + i * i
end

local result = {}
for key = 0, 16 do
  result[#result + 1] = string.format("%d:%d", key, counts[key])
end
print(table.concat(result, ","))
