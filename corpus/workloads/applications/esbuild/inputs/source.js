const catalog = [
  { name: "alpha", score: 14, tags: ["core", "parser"] },
  { name: "beta", score: 21, tags: ["runtime"] },
  { name: "gamma", score: 7, tags: ["core", "runtime"] },
];

function summarize(records, minimum) {
  const selected = records
    .filter((record) => record.score >= minimum)
    .map((record) => ({
      name: record.name.toUpperCase(),
      weighted: record.score * (record.tags.length + 1),
      category: record.tags.includes("core") ? "foundation" : "application",
    }));

  const totals = selected.reduce(
    (accumulator, record) => {
      accumulator.count += 1;
      accumulator.weighted += record.weighted;
      accumulator.names.push(record.name);
      return accumulator;
    },
    { count: 0, weighted: 0, names: [] },
  );

  return {
    ...totals,
    average: totals.count === 0 ? 0 : totals.weighted / totals.count,
    entries: selected.sort((left, right) => right.weighted - left.weighted),
  };
}

console.log(JSON.stringify(summarize(catalog, 10)));
