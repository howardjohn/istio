local g = import 'g.libsonnet';

local row = g.panel.row;

local grid = import 'lib-grid.libsonnet';
local dashboard = import './dashboard.libsonnet';
local panels = import './panels.libsonnet';
local variables = import './variables.libsonnet';
local queries = (import './queries.libsonnet').queries({
  container: '',
  pod: '',
  component: '',
  app: '',
});

dashboard.new('Istio Node Graph')
+ g.dashboard.withPanels(
panels.node.base('Traffic', queries.nodeGraph, 'Traffic') + {
      gridPos+: {
        h: 16,
        w: 24,
        y: 0,
      },
    }
)
+ g.dashboard.withUid(std.md5('istio-node.json'))
