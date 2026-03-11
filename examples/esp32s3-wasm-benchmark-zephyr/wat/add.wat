(module
  (func $main (param i32 i32) (result i32)
    (local $i i32)
    (local $acc i32)
    (block $break
      (loop $loop
        (br_if $break (i32.ge_u (local.get $i) (i32.const 50000)))
        (local.set $acc (i32.add (local.get $acc) (local.get $i)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)))
    (i32.const 0))
  (export "main" (func $main)))
