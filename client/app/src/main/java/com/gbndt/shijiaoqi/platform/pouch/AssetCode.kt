package com.gbndt.shijiaoqi.platform.pouch

/** 工艺/工程只读编号，跟 Go assetcode 同形；不当稳定身份。 */
object AssetCode {
    const val PREFIX_PROCESS = "GY"
    const val PREFIX_PROJECT = "GC"
    const val ORIGIN_WAN = "W"
    const val MAX_SEQ = 999_999L
    const val MAX_FACTORY = 99L
    const val MAX_CLIENT = 9_999L
    const val ERR_MISSING = "asset origin code is not assigned"
    const val ERR_CONFLICT = "asset code already exists"
    const val ERR_EXHAUSTED = "origin code exhausted"

    private val codeRe = Regex("^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$")
    private val clientOriginRe = Regex("^C[0-9]{4}$")

    fun prefix(kind: String): String = when (kind) {
        Pouch.KIND_PROCESS -> PREFIX_PROCESS
        Pouch.KIND_PROJECT -> PREFIX_PROJECT
        else -> throw PouchRejected(ERR_CONFLICT)
    }

    fun format(kind: String, origin: String, n: Long): String {
        val p = prefix(kind)
        if (n < 1 || n > MAX_SEQ) throw PouchRejected(ERR_EXHAUSTED)
        if (origin != ORIGIN_WAN && !validFactoryOrigin(origin) && !validClientOrigin(origin)) {
            throw PouchRejected(ERR_CONFLICT)
        }
        return "%s-%s-%06d".format(p, origin, n)
    }

    fun formatFactory(n: Long): String {
        if (n < 1 || n > MAX_FACTORY) throw PouchRejected(ERR_EXHAUSTED)
        return "F%02d".format(n)
    }

    fun formatClient(n: Long): String {
        if (n < 1 || n > MAX_CLIENT) throw PouchRejected(ERR_EXHAUSTED)
        return "C%04d".format(n)
    }

    fun valid(code: String): Boolean = codeRe.matches(code)

    fun matchKind(kind: String, code: String): Boolean {
        val p = try {
            prefix(kind)
        } catch (_: PouchRejected) {
            return false
        }
        return valid(code) && code.startsWith(p)
    }

    fun validFactoryOrigin(s: String): Boolean =
        s.length == 3 && s[0] == 'F' && s[1] in '0'..'9' && s[2] in '0'..'9' && s != "F00"

    fun validClientOrigin(s: String): Boolean = clientOriginRe.matches(s)
}
