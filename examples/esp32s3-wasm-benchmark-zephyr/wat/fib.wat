(module
  (func $main (param i32 i32) (result i32)
    (local $rep i32)
    (local $a i32)
    (local $b i32)
    (local $c i32)
    (local $j i32)
    (block $outer_break
      (loop $outer
        (br_if $outer_break (i32.ge_u (local.get $rep) (i32.const 500)))
        (local.set $a (i32.const 0))
        (local.set $b (i32.const 1))
        (local.set $j (i32.const 0))
        (block $inner_break
          (loop $inner
            (br_if $inner_break (i32.ge_u (local.get $j) (i32.const 30)))
            (local.set $c (i32.add (local.get $a) (local.get $b)))
            (local.set $a (local.get $b))
            (local.set $b (local.get $c))
            (local.set $j (i32.add (local.get $j) (i32.const 1)))
            (br $inner)))
        (local.set $rep (i32.add (local.get $rep) (i32.const 1)))
        (br $outer)))
    (i32.const 0))
  (export "main" (func $main)))
