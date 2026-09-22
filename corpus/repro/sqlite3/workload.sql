CREATE TABLE events (category TEXT NOT NULL, score INTEGER NOT NULL);
WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<1000)
INSERT INTO events SELECT CASE n%3 WHEN 0 THEN 'red' WHEN 1 THEN 'green' ELSE 'blue' END, (n*n)%97 FROM seq;
SELECT category, count(*), sum(score), min(score), max(score)
FROM events GROUP BY category ORDER BY category;
SELECT count(*), sum(a.score*b.score)
FROM events a JOIN events b ON a.rowid=b.rowid+1
WHERE a.category<>b.category;
