struct Person{let name:String;let scores:[Int];func greeting()->String{let total=scores.reduce(0,+);return "Hello, \(name)! Score: \(total)"}}
let people=[Person(name:"Ada",scores:[7,11]),Person(name:"Grace",scores:[5,13])]
for person in people{print(person.greeting())}
