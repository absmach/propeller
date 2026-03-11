BINS_H  := ../src/wasm_bins.h
TASKS_H := ../src/wasm_tasks.h

BINS_WATS  := cpu_stress.wat mem_stress.wat msg_stress.wat
TASKS_WATS := add.wat mul.wat fib.wat checksum.wat popcount.wat

all: $(BINS_H) $(TASKS_H)

$(BINS_H): $(BINS_WATS)
	python3 gen_headers.py --output $@ $(BINS_WATS)

$(TASKS_H): $(TASKS_WATS)
	python3 gen_headers.py --output $@ $(TASKS_WATS)

clean:
	rm -f *.wasm

.PHONY: all clean
