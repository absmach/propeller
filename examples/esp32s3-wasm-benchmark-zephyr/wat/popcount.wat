(module
  (func $main (param i32 i32) (result i32)
    (local $i i32)
    (local $n i32)
    (local $count i32)
    (block $outer_break
      (loop $outer
        (br_if $outer_break (i32.ge_u (local.get $i) (i32.const 50000)))
        (local.set $n (local.get $i))
        (block $inner_break
          (loop $inner
            (br_if $inner_break (i32.eqz (local.get $n)))
            (local.set $n (i32.and (local.get $n) (i32.sub (local.get $n) (i32.const 1))))
            (local.set $count (i32.add (local.get $count) (i32.const 1)))
            (br $inner)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $outer)))
    (i32.const 0))
  (export "main" (func $main)))
