counts = {}
for i in range(1, 5001):
    key = i % 17
    counts[key] = counts.get(key, 0) + i * i

print(",".join(f"{key}:{counts[key]}" for key in range(17)))
