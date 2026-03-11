(module
  (memory 1 1)
  (global $head (mut i32) (i32.const 0))
  (global $tail (mut i32) (i32.const 0))
  (func $main (param i32 i32) (result i32)
    (local $i i32)
    (local $slot i32)
    (block $prod_break
      (loop $prod
        (br_if $prod_break (i32.ge_s (local.get $i) (i32.const 1000)))
        (local.set $slot
          (i32.mul
            (i32.rem_u (global.get $head) (i32.const 64))
            (i32.const 4)))
        (i32.store (local.get $slot) (local.get $i))
        (global.set $head (i32.add (global.get $head) (i32.const 1)))
        (block $drain
          (br_if $drain
            (i32.lt_u
              (i32.sub (global.get $head) (global.get $tail))
              (i32.const 64)))
          (local.set $slot
            (i32.mul
              (i32.rem_u (global.get $tail) (i32.const 64))
              (i32.const 4)))
          (drop (i32.load (local.get $slot)))
          (global.set $tail (i32.add (global.get $tail) (i32.const 1))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $prod)))
    (i32.const 0))
  (export "main" (func $main)))
