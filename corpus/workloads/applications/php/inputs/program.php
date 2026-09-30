<?php
$counts = array_fill(0, 17, 0);
for ($i = 1; $i <= 5000; ++$i) {
    $counts[$i % 17] += $i * $i;
}
$result = [];
for ($key = 0; $key < 17; ++$key) {
    $result[] = $key . ':' . $counts[$key];
}
echo implode(',', $result), "\n";
