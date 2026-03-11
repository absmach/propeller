(module
  (global $acc (mut i32) (i32.const 2166136261))
  (func $main (param i32 i32) (result i32)
    (local $i i32)
    (local $h i32)
    (local.set $h (global.get $acc))
    (block $break
      (loop $loop
        (br_if $break (i32.ge_s (local.get $i) (i32.const 10000)))
        (local.set $h
          (i32.mul
            (i32.xor (local.get $h) (local.get $i))
            (i32.const 16777619)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)))
    (global.set $acc (local.get $h))
    (i32.const 0))
  (export "main" (func $main)))
