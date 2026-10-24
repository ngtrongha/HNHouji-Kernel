import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("namespace_header", Path(__file__).resolve().parents[3] / ".github/scripts/susfs_namespace_header.py")
header = importlib.util.module_from_spec(spec)
spec.loader.exec_module(header)

SOURCE = '#include <linux/fs_context.h>\n#include "pnode.h"\n#include "internal.h"\n' + header.HEADER + '\nstatic int unchanged_body;\n'

def patch(context):
    return 'diff --git a/fs/namespace.c b/fs/namespace.c\n--- a/fs/namespace.c\n+++ b/fs/namespace.c\n@@ -1,3 +1,4 @@\n #include "internal.h"\n' + context + '+#include <linux/susfs_def.h>\n \n'

class NamespaceHeaderTests(unittest.TestCase):
    def test_current_patch_preserves_stock_vendor_hook(self):
        prepared = header.prepare(SOURCE, patch(" " + header.HEADER))
        self.assertEqual(prepared, SOURCE)
        self.assertEqual(header.restore(prepared), SOURCE)

    def test_old_patch_temporarily_removes_then_restores_exact_source(self):
        prepared = header.prepare(SOURCE, patch(""))
        self.assertNotIn(header.HEADER, prepared)
        self.assertEqual(header.restore(prepared), SOURCE)
        self.assertEqual(header.restore(header.restore(prepared)), SOURCE)

    def test_missing_or_ambiguous_namespace_patch_is_rejected(self):
        for data in ["", "diff --git a/other.c b/other.c\n", patch("") + patch("")]:
            with self.assertRaises(ValueError):
                header.prepare(SOURCE, data)

    def test_other_file_context_cannot_choose_namespace_behavior(self):
        other = 'diff --git a/other.c b/other.c\n@@ -1,1 +1,2 @@\n ' + header.HEADER
        self.assertEqual(header.prepare(SOURCE, other + patch("")), SOURCE.replace(header.HEADER, ""))
        with self.assertRaises(ValueError):
            header.restore(SOURCE + header.HEADER)

if __name__ == "__main__":
    unittest.main()
