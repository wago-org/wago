counts = Hash.new(0)
(1..5000).each do |i|
  counts[i % 17] += i * i
end

puts (0...17).map { |key| "#{key}:#{counts[key]}" }.join(",")
