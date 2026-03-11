(module
  (memory 1 1)
  (func $main (param i32 i32) (result i32)
    (local $round i32)
    (local $i i32)
    (local $dummy i32)
    (block $outer_break
      (loop $outer
        (br_if $outer_break (i32.ge_s (local.get $round) (i32.const 100)))
        (local.set $i (i32.const 0))
        (block $write_break
          (loop $write
            (br_if $write_break (i32.ge_s (local.get $i) (i32.const 1024)))
            (i32.store8 (local.get $i)
              (i32.and (local.get $i) (i32.const 255)))
            (local.set $i (i32.add (local.get $i) (i32.const 1)))
            (br $write)))
        (local.set $i (i32.const 0))
        (block $read_break
          (loop $read
            (br_if $read_break (i32.ge_s (local.get $i) (i32.const 1024)))
            (local.set $dummy (i32.load8_u (local.get $i)))
            (local.set $i (i32.add (local.get $i) (i32.const 1)))
            (br $read)))
        (local.set $round (i32.add (local.get $round) (i32.const 1)))
        (br $outer)))
    (i32.const 0))
  (export "main" (func $main)))
