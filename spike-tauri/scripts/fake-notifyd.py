# A stand-in org.freedesktop.Notifications that "clicks" (ActionInvoked
# "default") every notification a second after it is shown. WP0 S7. Never merge.
import gi
gi.require_version('Gio', '2.0')
from gi.repository import Gio, GLib

XML = '''<node><interface name="org.freedesktop.Notifications">
<method name="Notify"><arg type="s" direction="in"/><arg type="u" direction="in"/><arg type="s" direction="in"/>
<arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="as" direction="in"/><arg type="a{sv}" direction="in"/>
<arg type="i" direction="in"/><arg type="u" direction="out"/></method>
<method name="CloseNotification"><arg type="u" direction="in"/></method>
<method name="GetCapabilities"><arg type="as" direction="out"/></method>
<method name="GetServerInformation"><arg type="s" direction="out"/><arg type="s" direction="out"/><arg type="s" direction="out"/><arg type="s" direction="out"/></method>
<signal name="ActionInvoked"><arg type="u"/><arg type="s"/></signal>
<signal name="NotificationClosed"><arg type="u"/><arg type="u"/></signal>
</interface></node>'''
node = Gio.DBusNodeInfo.new_for_xml(XML)
state = {'id': 0}

def call(conn, sender, path, iface, method, params, inv):
    if method == 'Notify':
        state['id'] += 1
        nid = state['id']
        args = params.unpack()
        print('[fake-notifyd] Notify', args[3], args[4], 'actions=', args[5], flush=True)
        inv.return_value(GLib.Variant('(u)', (nid,)))
        def click():
            conn.emit_signal(None, '/org/freedesktop/Notifications', 'org.freedesktop.Notifications', 'ActionInvoked', GLib.Variant('(us)', (nid, 'default')))
            conn.emit_signal(None, '/org/freedesktop/Notifications', 'org.freedesktop.Notifications', 'NotificationClosed', GLib.Variant('(uu)', (nid, 2)))
            print('[fake-notifyd] clicked', nid, flush=True)
            return False
        GLib.timeout_add(1000, click)
    elif method == 'GetCapabilities':
        inv.return_value(GLib.Variant('(as)', (['actions', 'body'],)))
    elif method == 'GetServerInformation':
        inv.return_value(GLib.Variant('(ssss)', ('fake', 'agentrq', '1', '1.2')))
    else:
        inv.return_value(None)

def acquired(conn, name):
    conn.register_object('/org/freedesktop/Notifications', node.interfaces[0], call, None, None)

Gio.bus_own_name(Gio.BusType.SESSION, 'org.freedesktop.Notifications', Gio.BusNameOwnerFlags.REPLACE, acquired, None, None)
GLib.MainLoop().run()
