"""Independent classic-BPF interpretation; no kernel filter installation."""
import struct
import unittest

import profiles


def interpret(program, number=117, architecture=0xc00000b7, args=(0, 1, 0, 0, 0, 0)):
    data = struct.pack("<iI7Q", number, architecture, 0, *args)
    accumulator, pc = 0, 0
    for _ in range(128):
        code, yes, no, operand = program[pc]
        if code == 0x20:
            accumulator = struct.unpack_from("<I", data, operand)[0]
            pc += 1
        elif code == 0x15:
            pc += 1 + (yes if accumulator == operand else no)
        elif code == 0x06:
            return operand
        else:
            raise AssertionError("unsupported generated opcode")
        if not 0 <= pc < len(program):
            raise AssertionError("jump out of program")
    raise AssertionError("unbounded filter")


class FilterTests(unittest.TestCase):
    def test_only_closed_ptrace_requests_and_regsets_on_tid_one(self):
        program = profiles.target_filter_program()
        allowed = {2, 7, 9, 17, 0x4202, 0x4206, 0x4207}
        for request in list(range(32)) + list(range(0x4200, 0x4210)) + [1 << 32 | 7]:
            for tid in (0, 1, 2, (1 << 32) + 1):
                for regset in (0, 1, 0x402, 0x403, (1 << 32) + 0x402):
                    expected = tid == 1 and (request in allowed or request == 0x4204 and regset in (1, 0x402) or request == 0x4205 and regset == 0x402)
                    with self.subTest(request=request, tid=tid, regset=regset):
                        self.assertEqual(interpret(program, args=(request, tid, regset, 0, 0, 0)), 0x7fff0000 if expected else 0x50001)

    def test_foreign_architecture_is_killed_and_post_admission_exec_denied(self):
        program = profiles.target_filter_program()
        for arch in (0x40000003, 0xc000003e, 0, 183):
            self.assertEqual(interpret(program, architecture=arch), 0x80000000)
        for number in (221, 281):
            self.assertEqual(interpret(program, number=number), 0x50001)
        self.assertEqual(interpret(program, number=63), 0x7fff0000)

    def test_generated_header_bytes_have_a_closed_bounded_shape(self):
        program = profiles.target_filter_program()
        self.assertLessEqual(len(program), 64)
        self.assertEqual(len(b"".join(struct.pack("<HBBI", *row) for row in program)), len(program) * 8)
        self.assertTrue(all(code in (0x20, 0x15, 0x06) for code, _, _, _ in program))


if __name__ == "__main__":
    unittest.main()
