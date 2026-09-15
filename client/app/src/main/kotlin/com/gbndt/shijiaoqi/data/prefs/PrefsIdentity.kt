package com.gbndt.shijiaoqi.data.prefs

import android.content.Context
import com.gbndt.shijiaoqi.data.session.IdentityStore

class PrefsIdentity(context: Context) : IdentityStore {
    private val prefs = context.applicationContext.getSharedPreferences("wmesh.identity", Context.MODE_PRIVATE)

    override var factoryUrl: String
        get() = prefs.getString("factoryUrl", "") ?: ""
        set(value) { prefs.edit().putString("factoryUrl", value).apply() }

    override var factoryId: String
        get() = prefs.getString("factoryId", "") ?: ""
        set(value) { prefs.edit().putString("factoryId", value).apply() }

    override var clientId: String
        get() = prefs.getString("clientId", "") ?: ""
        set(value) { prefs.edit().putString("clientId", value).apply() }

    override var clientShortCode: String
        get() = prefs.getString("clientShortCode", "") ?: ""
        set(value) { prefs.edit().putString("clientShortCode", value).apply() }
}
