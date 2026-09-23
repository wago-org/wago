limit = 2000000
value = 1
total = 0
even = 0
index = 0
while index < limit:
    value = (value + 48271) % 1000003
    total = (total + value) % 1000000007
    if value % 2 == 0:
        even += 1
    index += 1
print("%d,%d,%d,%d" % (limit, value, total, even))
